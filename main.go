package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultIndexInterval = 100000 // Default: create index point every 100k lines
	DefaultMemoryGB      = 2      // Default memory limit 2GB
)

// IndexEntry index entry structure
type IndexEntry struct {
	LineNumber int64 // Line number
	Offset     int64 // File offset
}

// chunkResult file chunk processing result
type chunkResult struct {
	workerID    int
	startOffset int64
	endOffset   int64
	indexes     []IndexEntry
	lineCount   int64
	err         error
}

// BufferCache read-ahead buffer cache
type BufferCache struct {
	data     []byte
	startPos int64
	endPos   int64
	valid    bool
	mu       sync.RWMutex
}

// IOStats IO statistics information
type IOStats struct {
	readCount    int64     // Read operation count
	writeCount   int64     // Write operation count
	bytesRead    int64     // Bytes read
	bytesWritten int64     // Bytes written
	startTime    time.Time // Start time
	mu           sync.Mutex
}

// BufferManager buffer manager
type BufferManager struct {
	file        *os.File
	bufferSize  int
	currentBuf  *BufferCache
	nextBuf     *BufferCache
	preloadChan chan int64
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	stats       *IOStats // IO statistics
}

// FastReader fast file reader
type FastReader struct {
	filename      string
	indexFile     string
	memoryLimit   int64 // Memory limit (bytes)
	indexInterval int64 // Index interval (lines)
	workerCount   int   // Number of concurrent workers
	verbose       bool  // Whether to show verbose information
	indexes       []IndexEntry
	totalLines    int64
	ioStats       *IOStats // IO statistics
	mu            sync.RWMutex
}

// NewFastReader creates a new fast reader
func NewFastReader(filename string, memoryGB int, indexInterval int64) *FastReader {
	indexFile := filename + ".frc"
	memoryLimit := int64(memoryGB) * 1024 * 1024 * 1024 // Convert to bytes

	return &FastReader{
		filename:      filename,
		indexFile:     indexFile,
		memoryLimit:   memoryLimit,
		indexInterval: indexInterval,
		indexes:       make([]IndexEntry, 0),
		ioStats:       &IOStats{startTime: time.Now()},
	}
}

// NewBufferManager creates a buffer manager
func NewBufferManager(file *os.File, bufferSize int, stats *IOStats) *BufferManager {
	ctx, cancel := context.WithCancel(context.Background())

	bm := &BufferManager{
		file:        file,
		bufferSize:  bufferSize,
		currentBuf:  &BufferCache{},
		nextBuf:     &BufferCache{},
		preloadChan: make(chan int64, 1),
		ctx:         ctx,
		cancel:      cancel,
		stats:       stats,
	}

	// Start preload goroutine
	bm.wg.Add(1)
	go bm.preloadWorker()

	return bm
}

// preloadWorker preload worker goroutine
func (bm *BufferManager) preloadWorker() {
	defer bm.wg.Done()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case startPos := <-bm.preloadChan:
			bm.loadBuffer(bm.nextBuf, startPos)
		}
	}
}

// loadBuffer loads buffer data
func (bm *BufferManager) loadBuffer(buf *BufferCache, startPos int64) {
	buf.mu.Lock()
	defer buf.mu.Unlock()

	// Allocate buffer
	if buf.data == nil || len(buf.data) != bm.bufferSize {
		buf.data = make([]byte, bm.bufferSize)
	}

	// Seek to specified position
	_, err := bm.file.Seek(startPos, io.SeekStart)
	if err != nil {
		buf.valid = false
		return
	}

	// Read data
	n, err := bm.file.Read(buf.data)
	if err != nil && err != io.EOF {
		buf.valid = false
		return
	}

	// Update IO statistics
	if bm.stats != nil {
		bm.stats.mu.Lock()
		bm.stats.readCount++
		bm.stats.bytesRead += int64(n)
		bm.stats.mu.Unlock()
	}

	buf.startPos = startPos
	buf.endPos = startPos + int64(n)
	buf.data = buf.data[:n] // Adjust slice length
	buf.valid = true
}

// GetBuffer gets buffer data at specified position
func (bm *BufferManager) GetBuffer(startPos int64) *BufferCache {
	// Check if current buffer contains required data
	bm.currentBuf.mu.RLock()
	if bm.currentBuf.valid && startPos >= bm.currentBuf.startPos && startPos < bm.currentBuf.endPos {
		defer bm.currentBuf.mu.RUnlock()
		return bm.currentBuf
	}
	bm.currentBuf.mu.RUnlock()

	// Check if next buffer contains required data
	bm.nextBuf.mu.RLock()
	if bm.nextBuf.valid && startPos >= bm.nextBuf.startPos && startPos < bm.nextBuf.endPos {
		bm.nextBuf.mu.RUnlock()
		// Swap buffers
		bm.currentBuf, bm.nextBuf = bm.nextBuf, bm.currentBuf

		// Preload next buffer
		nextStartPos := bm.currentBuf.endPos
		select {
		case bm.preloadChan <- nextStartPos:
		default:
		}

		return bm.currentBuf
	}
	bm.nextBuf.mu.RUnlock()

	// Need to reload current buffer
	bm.loadBuffer(bm.currentBuf, startPos)

	// Preload next buffer
	nextStartPos := startPos + int64(bm.bufferSize)
	select {
	case bm.preloadChan <- nextStartPos:
	default:
	}

	return bm.currentBuf
}

// Close closes buffer manager
func (bm *BufferManager) Close() {
	bm.cancel()
	close(bm.preloadChan)
	bm.wg.Wait()
}

// PrintIOStats prints IO statistics information
func (stats *IOStats) PrintIOStats() {
	if stats == nil {
		return
	}

	stats.mu.Lock()
	defer stats.mu.Unlock()

	duration := time.Since(stats.startTime)
	if duration == 0 {
		duration = time.Nanosecond // avoid division by zero
	}

	// Calculate frequency (operations per second)
	readFreq := float64(stats.readCount) / duration.Seconds()
	writeFreq := float64(stats.writeCount) / duration.Seconds()

	// Format bytes
	readMB := float64(stats.bytesRead) / (1024 * 1024)
	writtenMB := float64(stats.bytesWritten) / (1024 * 1024)

	fmt.Printf("\n=== IO Statistics ===\n")
	fmt.Printf("Total read operations: %d\n", stats.readCount)
	fmt.Printf("Total write operations: %d\n", stats.writeCount)
	fmt.Printf("Data read: %.2f MB\n", readMB)
	fmt.Printf("Data written: %.2f MB\n", writtenMB)
	fmt.Printf("Read frequency: %.2f ops/sec\n", readFreq)
	fmt.Printf("Write frequency: %.2f ops/sec\n", writeFreq)
	fmt.Printf("Total duration: %v\n", duration)
}

// calculateOptimalBufferSize calculates optimal buffer size
func (fr *FastReader) calculateOptimalBufferSize() int {
	// Calculate buffer size based on memory limit
	// Reserve memory for read-ahead buffers (two buffers)
	maxBufferSize := fr.memoryLimit / 4 // Reserve 25% memory for buffers

	// Set reasonable range
	minBufferSize := int64(64 * 1024)            // Minimum 64KB
	defaultBufferSize := int64(16 * 1024 * 1024) // Default 16MB

	if maxBufferSize < minBufferSize {
		return int(minBufferSize)
	}

	if maxBufferSize > defaultBufferSize {
		return int(defaultBufferSize)
	}

	return int(maxBufferSize)
}

// readLineFromBuffer 从缓冲区读取一行数据
func (fr *FastReader) readLineFromBuffer(bm *BufferManager, startOffset int64) ([]byte, int64, error) {
	buffer := bm.GetBuffer(startOffset)

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	if !buffer.valid {
		return nil, startOffset, fmt.Errorf("invalid buffer")
	}

	// 计算在缓冲区中的相对位置
	relativePos := startOffset - buffer.startPos
	if relativePos < 0 || relativePos >= int64(len(buffer.data)) {
		return nil, startOffset, fmt.Errorf("position out of buffer range")
	}

	// 在缓冲区中查找换行符
	data := buffer.data[relativePos:]
	for i, b := range data {
		if b == '\n' {
			// 找到换行符，返回包含换行符的行
			line := make([]byte, i+1)
			copy(line, data[:i+1])
			return line, startOffset + int64(i+1), nil
		}
	}

	// 没有找到换行符，可能需要跨缓冲区读取
	// 先返回当前缓冲区的剩余数据
	remainingData := make([]byte, len(data))
	copy(remainingData, data)
	nextOffset := buffer.endPos

	// 尝试从下一个缓冲区继续读取
	for {
		nextBuffer := bm.GetBuffer(nextOffset)
		nextBuffer.mu.RLock()

		if !nextBuffer.valid {
			nextBuffer.mu.RUnlock()
			// 到达文件末尾，返回剩余数据
			if len(remainingData) > 0 {
				return remainingData, nextOffset, io.EOF
			}
			return nil, nextOffset, io.EOF
		}

		// 在下一个缓冲区中查找换行符
		for i, b := range nextBuffer.data {
			if b == '\n' {
				// 找到换行符，合并数据
				line := make([]byte, len(remainingData)+i+1)
				copy(line, remainingData)
				copy(line[len(remainingData):], nextBuffer.data[:i+1])
				finalOffset := nextOffset + int64(i+1)
				nextBuffer.mu.RUnlock()
				return line, finalOffset, nil
			}
		}

		// 仍然没有找到换行符，继续合并数据
		newData := make([]byte, len(remainingData)+len(nextBuffer.data))
		copy(newData, remainingData)
		copy(newData[len(remainingData):], nextBuffer.data)
		remainingData = newData
		nextOffset = nextBuffer.endPos
		nextBuffer.mu.RUnlock()

		// 防止无限循环，设置合理的行长度限制
		if len(remainingData) > 1024*1024 { // 1MB行长度限制
			return remainingData, nextOffset, fmt.Errorf("line length exceeds limit")
		}
	}
}

func main() {
	// Define command line parameters with shortcuts
	var maxMem = flag.Int("max-mem", DefaultMemoryGB, "Maximum memory usage in GB")
	var maxMemShort = flag.Int("m", DefaultMemoryGB, "Maximum memory usage in GB (shorthand)")
	var indexIndent = flag.Int64("index-indent", DefaultIndexInterval, "Index interval in lines")
	var indexIndentShort = flag.Int64("i", DefaultIndexInterval, "Index interval in lines (shorthand)")
	var workers = flag.Int("workers", 0, "Number of concurrent workers (0=auto detect CPU cores)")
	var workersShort = flag.Int("w", 0, "Number of concurrent workers (shorthand)")
	var verbose = flag.Bool("verbose", false, "Show verbose information")
	var v = flag.Bool("v", false, "Show verbose information (shorthand)")
	var help = flag.Bool("help", false, "Show help information")
	var h = flag.Bool("h", false, "Show help information (shorthand)")

	// Custom Usage function
	flag.Usage = printUsage

	// Parse command line arguments
	flag.Parse()

	// Show help information
	if *help || *h {
		printUsage()
		return
	}

	// Get non-flag arguments
	args := flag.Args()

	// Parse filename and command from args
	filename, command, err := parseArgs(args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		printUsage()
		os.Exit(1)
	}

	// Validate file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("Error: File does not exist: %s\n", filename)
		os.Exit(1)
	}

	// Use shorthand values if main flags are default
	finalMaxMem := *maxMem
	if *maxMemShort != DefaultMemoryGB {
		finalMaxMem = *maxMemShort
	}

	finalIndexIndent := *indexIndent
	if *indexIndentShort != DefaultIndexInterval {
		finalIndexIndent = *indexIndentShort
	}

	finalWorkers := *workers
	if *workersShort != 0 {
		finalWorkers = *workersShort
	}

	// Validate parameters
	if finalMaxMem <= 0 {
		fmt.Printf("Error: Maximum memory usage must be greater than 0\n")
		os.Exit(1)
	}

	if finalIndexIndent <= 0 {
		fmt.Printf("Error: Index interval must be greater than 0\n")
		os.Exit(1)
	}

	reader := NewFastReader(filename, finalMaxMem, finalIndexIndent)

	// Store parameters for later use
	reader.workerCount = finalWorkers
	reader.verbose = *verbose || *v

	// Execute different operations based on command
	switch command {
	case "gen":
		// Generate index file
		fmt.Printf("Generating index for file %s...\n", filename)
		if reader.verbose {
			fmt.Printf("Config: memory limit %dGB, index interval %d lines\n", finalMaxMem, finalIndexIndent)
		}
		start := time.Now()
		if err := reader.GenerateIndex(); err != nil {
			fmt.Printf("Failed to generate index: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Index generation completed, elapsed: %v\n", time.Since(start))
		fmt.Printf("Index file: %s\n", reader.indexFile)

		// Show IO statistics in verbose mode
		if reader.verbose {
			reader.ioStats.PrintIOStats()
		}

	case "cnt":
		// Output line count
		if err := reader.LoadIndex(); err != nil {
			fmt.Printf("Failed to load index, generating new index...\n")
			if reader.verbose {
				fmt.Printf("Config: memory limit %dGB, index interval %d lines\n", finalMaxMem, finalIndexIndent)
			}
			if err := reader.GenerateIndex(); err != nil {
				fmt.Printf("Failed to generate index: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("%d\n", reader.totalLines)

	default:
		// Query line content
		if err := reader.LoadIndex(); err != nil {
			fmt.Printf("Index file does not exist or is corrupted, generating new index...\n")
			if reader.verbose {
				fmt.Printf("Config: memory limit %dGB, index interval %d lines\n", finalMaxMem, finalIndexIndent)
			}
			if err := reader.GenerateIndex(); err != nil {
				fmt.Printf("Failed to generate index: %v\n", err)
				os.Exit(1)
			}
		}

		if err := reader.QueryLines(command); err != nil {
			fmt.Printf("Query failed: %v\n", err)
			os.Exit(1)
		}
	}
}

// parseArgs parses non-flag arguments to extract filename and command
func parseArgs(args []string) (filename, command string, err error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("no arguments provided, please specify filename and command")
	}

	if len(args) == 1 {
		return "", "", fmt.Errorf("missing command, please specify operation (gen/cnt/line_query)")
	}

	if len(args) > 2 {
		return "", "", fmt.Errorf("too many arguments, only filename and command are allowed")
	}

	// Both arguments can be in any order
	// Try to determine which is filename and which is command
	arg1, arg2 := args[0], args[1]

	// Check if arg1 is a known command
	if isCommand(arg1) {
		return arg2, arg1, nil
	}

	// Check if arg2 is a known command
	if isCommand(arg2) {
		return arg1, arg2, nil
	}

	// If neither is a known command, assume first is filename, second is line query
	return arg1, arg2, nil
}

// isCommand checks if the argument is a known command
func isCommand(arg string) bool {
	return arg == "gen" || arg == "cnt"
}

// GenerateIndex 生成索引文件
func (fr *FastReader) GenerateIndex() error {
	file, err := os.Open(fr.filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}
	fileSize := fileInfo.Size()

	// 计算合适的缓冲区大小
	bufferSize := fr.calculateBufferSize(fileSize)

	fr.mu.Lock()
	fr.indexes = make([]IndexEntry, 0)
	fr.totalLines = 0
	fr.mu.Unlock()

	// 使用多协程处理大文件（降低触发阈值）
	if fileSize > 50*1024*1024 { // 大于50MB就使用并发处理
		if fr.verbose {
			fmt.Printf("Using concurrent mode for large file, buffer size: %d MB\n", bufferSize/(1024*1024))
		}
		return fr.generateIndexConcurrent(file, fileSize, bufferSize, fr.workerCount)
	}

	if fr.verbose {
		fmt.Printf("Using sequential mode for file, buffer size: %d KB\n", bufferSize/1024)
	}
	return fr.generateIndexSequential(file, bufferSize)
}

// calculateBufferSize 计算合适的缓冲区大小
func (fr *FastReader) calculateBufferSize(fileSize int64) int {
	// 基于内存限制和CPU核心数计算缓冲区大小
	cpuCount := int64(runtime.NumCPU())

	// 每个CPU核心分配的内存 = 总内存限制 / CPU核心数
	perCpuMemory := fr.memoryLimit / cpuCount

	// 缓冲区使用每个CPU分配内存的80%，留20%给其他操作
	bufferSize := perCpuMemory * 4 / 5

	// 设置合理的最小和最大值
	minBuffer := int64(1024 * 1024)            // 最小1MB
	maxBuffer := int64(2 * 1024 * 1024 * 1024) // 最大2GB

	if bufferSize < minBuffer {
		bufferSize = minBuffer
	}
	if bufferSize > maxBuffer {
		bufferSize = maxBuffer
	}

	// 对于小文件，不需要太大的缓冲区
	// 只有当文件非常小（小于10MB）时才限制缓冲区大小
	if fileSize < 10*1024*1024 && fileSize < bufferSize {
		bufferSize = fileSize / 4
		if bufferSize < minBuffer {
			bufferSize = minBuffer
		}
	}

	return int(bufferSize)
}

// generateIndexSequential 顺序生成索引
func (fr *FastReader) generateIndexSequential(file *os.File, bufferSize int) error {
	reader := bufio.NewReaderSize(file, bufferSize)
	var offset int64 = 0
	var lineNum int64 = 0
	var totalBytesRead int64 = 0

	// 添加第一个索引点（第1行的开始位置）
	fr.mu.Lock()
	fr.indexes = append(fr.indexes, IndexEntry{LineNumber: 1, Offset: 0})
	fr.mu.Unlock()

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read file: %v", err)
		}

		lineNum++
		lineLen := int64(len(line))
		offset += lineLen
		totalBytesRead += lineLen

		// 每indexInterval行创建一个索引点，记录下一个区间开始行的位置
		if lineNum%fr.indexInterval == 0 {
			fr.mu.Lock()
			fr.indexes = append(fr.indexes, IndexEntry{
				LineNumber: lineNum + 1, // 下一行的行号
				Offset:     offset,      // 下一行的开始位置
			})
			fr.mu.Unlock()
		}
	}

	// Update IO statistics
	if fr.ioStats != nil {
		fr.ioStats.mu.Lock()
		fr.ioStats.readCount++
		fr.ioStats.bytesRead += totalBytesRead
		fr.ioStats.mu.Unlock()
	}

	fr.mu.Lock()
	fr.totalLines = lineNum
	fr.mu.Unlock()

	return fr.saveIndex()
}

// generateIndexConcurrent 真正的并发生成索引
func (fr *FastReader) generateIndexConcurrent(file *os.File, fileSize int64, bufferSize int, workerCount int) error {
	// 显示内存分配信息
	cpuCount := runtime.NumCPU()
	perCpuMemory := fr.memoryLimit / int64(cpuCount)
	if fr.verbose {
		fmt.Printf("CPU cores: %d, memory per core: %d GB, actual buffer: %d MB\n",
			cpuCount, perCpuMemory/(1024*1024*1024), bufferSize/(1024*1024))
	}

	// 设置并发数
	var numWorkers int
	if workerCount > 0 {
		// 使用用户指定的并发数
		numWorkers = workerCount
		if fr.verbose {
			fmt.Printf("Using user-specified worker count: %d\n", numWorkers)
		}
	} else {
		// 自动检测并发数
		numWorkers = cpuCount

		// 对于高核心数的服务器，可以使用更多并发
		// 但要考虑文件句柄和内存的限制
		maxWorkers := 64 // 最大64个并发线程
		if numWorkers > maxWorkers {
			numWorkers = maxWorkers
		}

		// 确保至少有4个工作线程
		if numWorkers < 4 {
			numWorkers = 4
		}
		if fr.verbose {
			fmt.Printf("Auto-detected worker count: %d (based on %d CPU cores)\n", numWorkers, cpuCount)
		}
	}

	if fr.verbose {
		fmt.Printf("Using %d concurrent workers to process file chunks\n", numWorkers)
	}

	// 计算每个工作线程处理的文件块大小
	chunkSize := fileSize / int64(numWorkers)

	// 结果通道
	results := make(chan chunkResult, numWorkers)
	var wg sync.WaitGroup

	// 预先计算所有工作线程的边界，避免重叠
	boundaries := make([]int64, numWorkers+1)
	boundaries[0] = 0
	boundaries[numWorkers] = fileSize

	// 计算中间边界点，并调整到行边界
	for i := 1; i < numWorkers; i++ {
		roughPos := int64(i) * chunkSize
		// 找到最近的行边界
		adjustedPos, err := fr.findNextLineStart(file, roughPos)
		if err != nil {
			return fmt.Errorf("failed to adjust boundary: %v", err)
		}
		boundaries[i] = adjustedPos
	}

	if fr.verbose {
		fmt.Printf("Boundary adjustment result:")
		for i := 0; i <= numWorkers; i++ {
			fmt.Printf(" %d", boundaries[i])
		}
		fmt.Printf("\n")
	}

	// 启动并发工作线程
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			startPos := boundaries[workerID]
			endPos := boundaries[workerID+1]

			result := fr.processFileChunk(workerID, startPos, endPos, bufferSize)
			results <- result
		}(i)
	}

	// 等待所有工作线程完成
	go func() {
		wg.Wait()
		close(results)
	}()

	// 收集所有结果
	var allResults []chunkResult
	var totalLines int64

	for result := range results {
		if result.err != nil {
			return fmt.Errorf("worker %d failed: %v", result.workerID, result.err)
		}
		allResults = append(allResults, result)
		totalLines += result.lineCount
	}

	// 按工作线程ID排序结果，确保索引顺序正确
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].workerID < allResults[j].workerID
	})

	// 合并所有索引，处理边界不规则问题
	fr.mu.Lock()
	fr.indexes = make([]IndexEntry, 0)
	fr.indexes = append(fr.indexes, IndexEntry{LineNumber: 1, Offset: 0}) // 第一个索引点

	var currentLineOffset int64 = 0
	for _, result := range allResults {
		for _, index := range result.indexes {
			// 调整行号，加上前面所有块的行数
			adjustedIndex := IndexEntry{
				LineNumber: index.LineNumber + currentLineOffset,
				Offset:     index.Offset,
			}
			fr.indexes = append(fr.indexes, adjustedIndex)
		}
		currentLineOffset += result.lineCount
	}

	fr.totalLines = totalLines
	fr.mu.Unlock()

	if fr.verbose {
		fmt.Printf("Concurrent index generation completed, processed %d lines, generated %d index points\n", totalLines, len(fr.indexes))
	}

	return fr.saveIndex()
}

// processFileChunk 处理文件块，实现真正的并行索引生成
func (fr *FastReader) processFileChunk(workerID int, startPos, endPos int64, bufferSize int) chunkResult {
	// 打开独立的文件句柄
	file, err := os.Open(fr.filename)
	if err != nil {
		return chunkResult{workerID: workerID, err: fmt.Errorf("打开文件失败：%v", err)}
	}
	defer file.Close()

	// 边界已经预先调整好，直接使用
	actualStart := startPos
	actualEnd := endPos

	// 定位到实际起始位置
	_, err = file.Seek(actualStart, 0)
	if err != nil {
		return chunkResult{workerID: workerID, err: fmt.Errorf("failed to seek file: %v", err)}
	}

	reader := bufio.NewReaderSize(file, bufferSize)
	var indexes []IndexEntry
	var lineCount int64 = 0
	var currentOffset int64 = actualStart
	var totalBytesRead int64 = 0

	// 处理这个块中的所有行
	for currentOffset < actualEnd {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return chunkResult{workerID: workerID, err: fmt.Errorf("读取文件失败：%v", err)}
		}

		lineCount++
		lineLen := int64(len(line))
		totalBytesRead += lineLen

		// 每indexInterval行创建一个索引点（相对于这个块的开始）
		if lineCount%fr.indexInterval == 0 {
			indexes = append(indexes, IndexEntry{
				LineNumber: lineCount + 1,           // 下一行的行号（相对于块开始）
				Offset:     currentOffset + lineLen, // 下一行的开始位置
			})
		}

		currentOffset += lineLen
	}

	// Update IO statistics
	if fr.ioStats != nil {
		fr.ioStats.mu.Lock()
		fr.ioStats.readCount++
		fr.ioStats.bytesRead += totalBytesRead
		fr.ioStats.mu.Unlock()
	}

	if fr.verbose {
		fmt.Printf("Worker %d: processed range %d-%d, lines %d, indexes %d\n",
			workerID, actualStart, actualEnd, lineCount, len(indexes))
	}

	return chunkResult{
		workerID:    workerID,
		startOffset: actualStart,
		endOffset:   actualEnd,
		indexes:     indexes,
		lineCount:   lineCount,
		err:         nil,
	}
}

// findLineStart 找到指定位置最近的行起始位置
func (fr *FastReader) findLineStart(file *os.File, pos int64) (int64, error) {
	if pos == 0 {
		return 0, nil // 文件开头就是行起始
	}

	// 从指定位置向前搜索，找到最近的换行符
	searchStart := pos - 1024 // 向前搜索1KB
	if searchStart < 0 {
		searchStart = 0
	}

	_, err := file.Seek(searchStart, 0)
	if err != nil {
		return 0, err
	}

	buffer := make([]byte, pos-searchStart+1)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return 0, err
	}

	// 从后往前找换行符
	for i := int64(n) - 1; i >= 0; i-- {
		if buffer[i] == '\n' {
			return searchStart + i + 1, nil // 返回换行符后的位置
		}
	}

	return searchStart, nil // 如果没找到换行符，返回搜索起始位置
}

// findLineEnd 找到指定位置最近的行结束位置
func (fr *FastReader) findLineEnd(file *os.File, pos int64) (int64, error) {
	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := fileInfo.Size()

	if pos >= fileSize {
		return fileSize, nil // 文件末尾
	}

	// 从指定位置向后搜索，找到最近的换行符
	_, err = file.Seek(pos, 0)
	if err != nil {
		return 0, err
	}

	buffer := make([]byte, 1024) // 向后搜索1KB
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return 0, err
	}

	// 从前往后找换行符
	for i := 0; i < n; i++ {
		if buffer[i] == '\n' {
			return pos + int64(i) + 1, nil // 返回换行符后的位置
		}
	}

	// 如果没找到换行符，继续向后搜索
	searchEnd := pos + 1024
	if searchEnd > fileSize {
		searchEnd = fileSize
	}

	return searchEnd, nil
}

// findNextLineStart 找到指定位置之后的下一个行起始位置
func (fr *FastReader) findNextLineStart(file *os.File, pos int64) (int64, error) {
	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := fileInfo.Size()

	if pos >= fileSize {
		return fileSize, nil // 文件末尾
	}

	// 从指定位置向后搜索，找到最近的换行符
	_, err = file.Seek(pos, 0)
	if err != nil {
		return 0, err
	}

	buffer := make([]byte, 1024) // 向后搜索1KB
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return 0, err
	}

	// 从前往后找换行符
	for i := 0; i < n; i++ {
		if buffer[i] == '\n' {
			return pos + int64(i) + 1, nil // 返回换行符后的位置
		}
	}

	// 如果没找到换行符，继续向后搜索
	searchEnd := pos + 1024
	if searchEnd > fileSize {
		searchEnd = fileSize
	}

	return searchEnd, nil
}

// saveIndex 保存索引到文件
func (fr *FastReader) saveIndex() error {
	file, err := os.Create(fr.indexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file: %v", err)
	}
	defer file.Close()

	// 统计写入字节数
	var bytesWritten int64

	// 写入版本号和总行数
	if err := binary.Write(file, binary.LittleEndian, int64(1)); err != nil { // 版本号
		return err
	}
	bytesWritten += 8

	if err := binary.Write(file, binary.LittleEndian, fr.totalLines); err != nil {
		return err
	}
	bytesWritten += 8

	// 写入索引数量
	indexCount := int64(len(fr.indexes))
	if err := binary.Write(file, binary.LittleEndian, indexCount); err != nil {
		return err
	}
	bytesWritten += 8

	// 写入索引数据
	for _, index := range fr.indexes {
		if err := binary.Write(file, binary.LittleEndian, index.LineNumber); err != nil {
			return err
		}
		bytesWritten += 8

		if err := binary.Write(file, binary.LittleEndian, index.Offset); err != nil {
			return err
		}
		bytesWritten += 8
	}

	// Update IO statistics
	if fr.ioStats != nil {
		fr.ioStats.mu.Lock()
		fr.ioStats.writeCount++
		fr.ioStats.bytesWritten += bytesWritten
		fr.ioStats.mu.Unlock()
	}

	return nil
}

// LoadIndex loads index file
func (fr *FastReader) LoadIndex() error {
	file, err := os.Open(fr.indexFile)
	if err != nil {
		return fmt.Errorf("failed to open index file: %v", err)
	}
	defer file.Close()

	// Read version number
	var version int64
	if err := binary.Read(file, binary.LittleEndian, &version); err != nil {
		return fmt.Errorf("failed to read version: %v", err)
	}
	if version != 1 {
		return fmt.Errorf("unsupported index file version: %d", version)
	}

	// Read total line count
	if err := binary.Read(file, binary.LittleEndian, &fr.totalLines); err != nil {
		return fmt.Errorf("failed to read total lines: %v", err)
	}

	// Read index count
	var indexCount int64
	if err := binary.Read(file, binary.LittleEndian, &indexCount); err != nil {
		return fmt.Errorf("failed to read index count: %v", err)
	}

	// Read index data
	fr.indexes = make([]IndexEntry, indexCount)
	for i := int64(0); i < indexCount; i++ {
		if err := binary.Read(file, binary.LittleEndian, &fr.indexes[i].LineNumber); err != nil {
			return fmt.Errorf("failed to read index line number: %v", err)
		}
		if err := binary.Read(file, binary.LittleEndian, &fr.indexes[i].Offset); err != nil {
			return fmt.Errorf("failed to read index offset: %v", err)
		}
	}

	return nil
}

// QueryLines 查询指定行内容
func (fr *FastReader) QueryLines(query string) error {
	// 解析查询参数
	startLine, endLine, err := fr.parseQuery(query)
	if err != nil {
		return fmt.Errorf("failed to parse query parameters: %v", err)
	}

	// 转换负数索引
	if startLine < 0 {
		startLine = fr.totalLines + startLine + 1
	}
	if endLine < 0 {
		endLine = fr.totalLines + endLine + 1
	}

	// 验证行号范围
	if startLine < 1 || startLine > fr.totalLines {
		return fmt.Errorf("start line number out of range: %d (total lines: %d)", startLine, fr.totalLines)
	}
	if endLine < 1 || endLine > fr.totalLines {
		return fmt.Errorf("end line number out of range: %d (total lines: %d)", endLine, fr.totalLines)
	}
	if startLine > endLine {
		return fmt.Errorf("start line number cannot be greater than end line number: %d > %d", startLine, endLine)
	}

	// 读取并输出指定行
	return fr.readLines(startLine, endLine)
}

// parseQuery 解析查询字符串
func (fr *FastReader) parseQuery(query string) (int64, int64, error) {
	if strings.Contains(query, ":") {
		// 范围查询，如 "1:100" 或 "-10:-1"
		parts := strings.Split(query, ":")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("invalid range format: %s", query)
		}

		start, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start line number: %s", parts[0])
		}

		end, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end line number: %s", parts[1])
		}

		return start, end, nil
	} else {
		// 单行查询
		line, err := strconv.ParseInt(query, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid line number: %s", query)
		}
		return line, line, nil
	}
}

// readLines 读取指定范围的行
func (fr *FastReader) readLines(startLine, endLine int64) error {
	file, err := os.Open(fr.filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// 找到起始位置的索引
	startIndex := fr.findNearestIndex(startLine)

	// 计算合适的缓冲区大小（基于内存限制）
	bufferSize := fr.calculateOptimalBufferSize()

	// 创建缓冲区管理器
	bufferManager := NewBufferManager(file, bufferSize, fr.ioStats)
	defer bufferManager.Close()

	// 从索引位置开始读取
	currentOffset := startIndex.Offset
	currentLine := startIndex.LineNumber

	// 跳过到目标起始行
	for currentLine < startLine {
		line, newOffset, err := fr.readLineFromBuffer(bufferManager, currentOffset)
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("unexpected end of file")
			}
			return fmt.Errorf("failed to read file: %v", err)
		}
		currentOffset = newOffset
		currentLine++
		_ = line // 跳过这行，不输出
	}

	// 读取并输出目标行
	for currentLine <= endLine {
		line, newOffset, err := fr.readLineFromBuffer(bufferManager, currentOffset)
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					// 最后一行没有换行符
					fmt.Print(string(line))
				}
				break
			}
			return fmt.Errorf("failed to read file: %v", err)
		}

		fmt.Print(string(line))
		currentOffset = newOffset
		currentLine++
	}

	return nil
}

// findNearestIndex 找到最接近指定行号的索引
func (fr *FastReader) findNearestIndex(lineNumber int64) IndexEntry {
	if len(fr.indexes) == 0 {
		return IndexEntry{LineNumber: 0, Offset: 0}
	}

	// 二分查找最接近的索引
	left, right := 0, len(fr.indexes)-1
	bestIndex := fr.indexes[0]

	for left <= right {
		mid := (left + right) / 2
		if fr.indexes[mid].LineNumber <= lineNumber {
			bestIndex = fr.indexes[mid]
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	return bestIndex
}

func printUsage() {
	fmt.Println("FastReader - Fast file reader for large files")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  ./fastreader [options] <filename> <command>")
	fmt.Println("  ./fastreader [options] <command> <filename>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  gen                                    # Generate index file")
	fmt.Println("  cnt                                    # Show file line count")
	fmt.Println("  <line_number>                          # Read specific line(s)")
	fmt.Println()
	fmt.Println("Line number formats:")
	fmt.Println("  1                                      # Line 1")
	fmt.Println("  1:100                                  # Lines 1 to 100")
	fmt.Println("  -1                                     # Last line")
	fmt.Println("  -10:-1                                 # Last 10 lines")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --max-mem <value>, -m <value>          # Maximum memory usage in GB (default 2)")
	fmt.Println("  --index-indent <value>, -i <value>     # Index interval in lines (default 100000)")
	fmt.Println("  --workers <value>, -w <value>          # Number of concurrent workers (0=auto detect)")
	fmt.Println("  --verbose, -v                          # Show verbose information")
	fmt.Println("  --help, -h                             # Show help information")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ./fastreader data.csv gen              # Generate index with default settings")
	fmt.Println("  ./fastreader -m 4 data.csv gen        # Generate index with 4GB memory")
	fmt.Println("  ./fastreader gen -i 50000 data.csv    # Create index every 50k lines")
	fmt.Println("  ./fastreader data.csv 1                # Read line 1")
	fmt.Println("  ./fastreader data.csv 1:100            # Read lines 1-100")
	fmt.Println()
	fmt.Printf("Default settings: memory limit %dGB, index interval %d lines, CPU cores %d\n",
		DefaultMemoryGB, DefaultIndexInterval, runtime.NumCPU())
}
