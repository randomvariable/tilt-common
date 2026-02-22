# Analyzing Exported Profiles

The observability export CLI downloads pprof-format CPU and memory profiles
from Parca as `.pb.gz` files. These files are standard Go pprof protocol
buffer archives and can be analyzed directly with `go tool pprof`.

## Prerequisites

- Go installed (1.21+)
- Optional: [Graphviz](https://graphviz.org/) for flamegraph and graph
  visualization (`sudo apt install graphviz` / `brew install graphviz`)

## Basic Usage

### Interactive mode

Open a profile in the interactive pprof shell:

```bash
go tool pprof observability-export/profiles/process_cpu_cpu_nanoseconds-2026-02-14T153000.pb.gz
```

Common commands inside the interactive prompt:

| Command | Description |
|---------|-------------|
| `top` | Show top functions by resource usage |
| `top -cum` | Show top functions by cumulative usage |
| `list <func>` | Show annotated source for a function |
| `web` | Open flamegraph in browser (requires graphviz) |
| `svg > output.svg` | Save flamegraph as SVG |
| `peek <func>` | Show callers and callees of a function |
| `disasm <func>` | Show disassembly for a function |
| `quit` | Exit |

### Web UI mode

Launch a browser-based viewer with interactive flamegraphs, source annotations,
and graph visualizations:

```bash
go tool pprof -http=:6060 observability-export/profiles/process_cpu_cpu_nanoseconds-2026-02-14T153000.pb.gz
```

This starts a local HTTP server on port 6060 and opens the pprof web interface.
The web UI provides a richer experience than the command line, including:

- Interactive flamegraphs with zoom and search
- Top functions table with sorting
- Source-annotated call graphs
- Graph and peek views

To analyze memory profiles in the web UI:

```bash
go tool pprof -http=:6060 observability-export/profiles/memory_inuse_space_bytes-2026-02-14T153000.pb.gz
```

### Direct commands

Run analysis without entering interactive mode:

```bash
# Top functions across all CPU profiles
go tool pprof -top observability-export/profiles/process_cpu*.pb.gz

# Generate an SVG flamegraph
go tool pprof -svg observability-export/profiles/process_cpu*.pb.gz > cpu-flamegraph.svg

# Generate a text call graph
go tool pprof -text observability-export/profiles/process_cpu*.pb.gz
```

### Comparing profiles

Diff two profiles to see what changed between collection windows:

```bash
go tool pprof -diff_base=before.pb.gz after.pb.gz
```

Inside the interactive shell after diffing, `top` and `list` show the delta
between the two profiles, making it straightforward to identify regressions.

You can also compare profiles in the web UI for a visual diff:

```bash
go tool pprof -http=:6060 -diff_base=before.pb.gz after.pb.gz
```

The flamegraph highlights increases in red and decreases in green, making
regressions immediately visible.

To compare profiles across different export windows (e.g., before and after a
code change), export once before the change and once after, then diff the
corresponding `.pb.gz` files:

```bash
go tool pprof -http=:6060 \
  -diff_base=observability-export/profiles/process_cpu_cpu_nanoseconds-2026-02-14T150000.pb.gz \
  observability-export/profiles/process_cpu_cpu_nanoseconds-2026-02-14T160000.pb.gz
```

## Tips

- Install graphviz for the `web` and `svg` commands. Without it, only
  text-based output (`top`, `list`, `text`) is available.
- When analyzing memory profiles, use `-alloc_space` or `-inuse_space` to
  switch between allocation-total and live-heap views:
  ```bash
  go tool pprof -inuse_space observability-export/profiles/memory*.pb.gz
  ```
- Multiple `.pb.gz` files passed on the command line are merged, which is
  useful for aggregating samples across several export windows.

## Further Reading

- [Go pprof documentation](https://pkg.go.dev/net/http/pprof)
- [google/pprof on GitHub](https://github.com/google/pprof)
- [Profiling Go Programs (Go Blog)](https://go.dev/blog/pprof)
