# FastReader - Fast file reader for large files

FastReader is a high-performance Go program designed for processing very large text files (such as CSV files). It creates index files for fast line-level access with multi-threading support and memory management.

## Features

- **High Performance**: Multi-threaded processing with optimized memory usage
- **Index Mechanism**: Creates index points every 100k lines for fast positioning
- **Memory Management**: Configurable memory limit (default 2GB)
- **Flexible Queries**: Single line, range, negative indexing (Python-like)
- **Auto Indexing**: Automatic .frc index file generation and management
- **Fast Statistics**: Quick file line count

## Installation

```bash
# Clone and build
git clone https://github.com/FeverKing/fastreader
cd fastreader
go build -o fastreader main.go

# Or use Makefile
make build # Build For Current Platform
make linux-amd64 # Linux x64
make darwin-arm64 # MacOS Apple Silicon
...
```

## Usage

```bash
# Generate index
./fastreader data.csv gen

# Get line count
./fastreader data.csv cnt

# Read specific lines
./fastreader data.csv 1          # Line 1
./fastreader data.csv 1:100      # Lines 1-100
./fastreader data.csv -1         # Last line
./fastreader data.csv -10:-1     # Last 10 lines
```

## Options

```bash
-m, --max-mem <value>     # Memory limit in GB (default: 2)
-i, --index-indent <value> # Index interval in lines (default: 100000)
-w, --workers <value>     # Number of workers (default: auto)
-v, --verbose            # Show detailed information
-h, --help               # Show help
```

## Examples

```bash
# Basic usage
./fastreader data.csv gen
./fastreader data.csv 1:1000

# With options
./fastreader -m 4 -w 8 data.csv gen
./fastreader -v data.csv gen

# Flexible argument order
./fastreader gen -m 4 data.csv
./fastreader data.csv -i 50000 gen
```

## Performance

- **Large files**: Handles multi-GB files efficiently
- **Fast queries**: O(1) line access with indexing
- **Memory efficient**: Configurable memory limits
- **Multi-threaded**: Automatic CPU core detection

## License

MIT License