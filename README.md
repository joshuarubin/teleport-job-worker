# teleport-job-worker

Implement a prototype job worker service that provides an API to run arbitrary Linux processes

## Build Instructions

### Prerequisites

- A working Go environment 1.22 or greater
- [Install](https://buf.build/docs/installation) the `buf` cli
- Install `stringer`

```sh
go install golang.org/x/tools/cmd/stringer@latest
```

### Code Generation

From the root of the repo. Note that the generated files are committed and kept up-to-date.

```sh
buf generate
go generate ./...
```
