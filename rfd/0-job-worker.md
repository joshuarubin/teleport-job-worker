---
authors: Joshua Rubin (me@jawa.dev)
state: draft
---

# RFD 0 - Prototype Job Worker Service

## What

Implement a prototype job worker service that provides an API to run arbitrary Linux processes.

## Details

### Design Approach

Follow the requirements of [the challenge](https://github.com/gravitational/careers/blob/main/challenges/systems/challenge-1.md) as closely as possible and do not unnecessarily expand the scope.

### Proposed API

The gRPC API is defined in `jobworker.proto` and the worker library interface is defined in `worker.go`.

#### Worker Library

The worker library defines a simple interface with methods to start, stop, query status and get the output of a job. The implementation is responsible for starting jobs, keeping track of their output, status and exit codes.

##### cgroups

The library implementation will be configured at instantiation with limits that can be parsed into values for `cpu.max`, `memory.max` and `io.max`. These values will originate from runtime flags provided to the server with the following defaults:

- `cpu.max`: `25000 100000`
- `memory.max`: `128M`
- `io.max`: `$MAJ:$MIN riops=100 wiops=10` (for each block device)

All processes will be started in a new cgroup regardless of the values of these limits. Note that for expediency, it is assumed that cgroups v2 are in use on the system running the service.

##### namespaces

The library implementation will be configured such that it knows how to execute the server binary as a child process, with the proper clone and unshare flags to execute in a new pid, mount and network namespace, and call back into the library (with the `StartJobChild()` method) to execute the given command for the job. This library instance is obviously separate from the main server process, but due to being reexecuted with the same flags, just a different command (`child` instead of `serve`, see CLI UX below), it will retain all the necessary configuration.

##### Job Execution

The server will initially reexecute itself, using `exec.Command()`, as follows:

```sh
/proc/self/exe child [serve flags] -- command [args]...
```

When calling this, it will set the command's `SysProcAttr` with:

```go
syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWPID | // New pid namespace
        syscall.CLONE_NEWNS | // New mount namespace group
        syscall.CLONE_NEWNET, // New network namespace
    Unshareflags: syscall.CLONE_NEWNS, // Isolate process mounts from host
}
```

In order to provide proper accounting of the job the server will internally track of the execution with:

- buffers for `stdout` and `stderr`
- process status (e.g. running, complete)
- exit code

Once reexecuted, the `child` command has several jobs to do. It has to remount the `/proc` filesystem, create and configure the cgroup, set the cgroup for the pid and fully replace the execution, using `syscall.Exec()`, with the job binary.

##### Job Status

Job status is extremely simple and only returns a status code and an optional exit_code. The status code is one of:

- running: the job has been started and has not yet completed
- complete: the job completed successfully on its own
- stopped: the job was manually stopped, this takes precedence over complete

##### Job Output Storage

Output for each job will be maintained, in memory, as long as the worker implementation exists. This poses a potential memory leak, but for the purposes of this challenge, is an acceptable solution. Later designs would likely need to write job output to disk or possibly even remote storage.

##### Job Output Streaming

The worker library streams job output through an `io.ReadCloser`. The server will indicate the end of the stream, if the job has completed, with the `io.EOF` error. The client may, at any time, close the stream to indicate that it is disconnecting. The client must always close the stream when it no longer needs it to prevent memory leaks.

Internally, the library will maintain a buffer containing the job output. The job itself will be configured to use this buffer for `stdout` and `stderr` and the `Write()` method will be goroutine safe. When a client calls `JobOutput()` the library will create a new `io.Reader` that independently, and with goroutine safety, reads through to the end of the output buffer. If the job has already completed at this time, `io.EOF` will be returned. If not, subsequent calls to `Read()` will block until there is either more output to return or the job ends in which case `io.EOF` is returned. When the job calls `Write()` it will signal connected readers that new data has been written to the output buffer. The readers will in turn be able to unblock and return their `Read()` calls to connected clients at that time.

##### Stopping Jobs

StopJob is an asynchronous request that takes an optional timeout. If the timeout is greater than 0, it will cause the library to first issue a `SIGINT` and wait up to `timeout` for the job to complete. If the job doesn't complete after `timeout`, or timeout was `0`, then the job is terminated with `SIGKILL`. The library returns a channel that will be closed when the job completes.

##### Identifiers

JobID is an opaque identifier that is internally mapped to job in the library. Since pid is not guaranteed to be unique, nothing internal to the library will be keyed off of the pid. The library must still track the pid though in order to signal the process to stop, for example. The `go.jetify.com/typeid` library is used to generate job ids.

UserID can be any unique value as far as the library is concerned. See the section on Authorization below for more information.

#### gRPC API

The gRPC API follows the worker library fairly closely. The only significant difference is that it does not require the user id in the messages because user id is the client certificate's subject (using `(*x509.Certificate).Subject.String()`), not explicitly provided. Additionally, `StopJob()` in the library returns a channel that closes when the job completes. The gRPC API does not provide a facility for notifying the client when the job completes.

### Security Considerations

#### Authentication

mTLS is used for authentication. The server will be configured with a ca certificate that it expects all client certificates to be signed by. All connections presenting a certificate, signed by the configured ca certificate, are authenticated, all other connections are not.

#### TLS

The following TLS configuration will be used, where `clientCAs` includes the ca cert that should sign all client certs and `crt` contains the server's cert and private key. Note that cipher suites are not used because they are [not configurable](https://go.dev/blog/tls-cipher-suites) in Go when using TLS 1.3. This configuration requires and validates client certificates for all connections.

```go
tls.Config{
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    clientCAs,
    Certificates: []tls.Certificate{crt},
    MinVersion:   tls.VersionTLS13,
}
```

For the purposes of testing, and as general good security practice, keys should be generated with, and certificates should be signed with, modern algorithms according to current best practices. A recommendable choice, when this is being written, is to use to use an ECC private key generated with the NIST P-256 curve and to use SHA256 as the signature hash algorithm. The P-256 curve is implemented as a constant-time algorithm in the Go standard library. While this recommendation is not necessarily the state of the art with regards to security, it remains a good option factoring in client compatibility as it is very broadly deployed. To increase security at the cost of compatibility, consider using Curve25519 instead. Additionally, consider using certificates with short expirations, 3 months or less, and build in automated processes to renew and deploy them before expiration.

#### Authorization

The subject of the client certificate (using `(*x509.Certificate).Subject.String()`) is used as a unique user identifier and is implicitly required for all requests. It is used for authorization such that only the user that starts a job is permitted to stop it, get its status or output. This way the client doesn't have to explicitly provide any other kind of identification.

#### Non-Considerations

At its core, this is a very dangerous service. It provides a facility for remote execution and, since it needs to be privileged to set up the namespaces, to do so as root. There are a number of things that could be done to make this safer, but are not requirements of the challenge, so they will not be implemented.

Some examples of security improvements in a future version would be:

- dropping privileges before executing a job
- pivot_root into a separate, read-only filesystem
- better handling of commands and arguments (e.g. proper escaping)

### CLI UX

A single binary, `job-worker` is used for all actions. It has several subcommands for each role. All configuration is done via cli flags.

#### root level usage output

```
A prototype job worker service that provides an api to run arbitrary linux processes

Usage:
  job-worker [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  output      Stream the output of a job on the job-worker server
  serve       Start the job-worker server and listen for connections
  start       Start a job on the job-worker server
  status      Get the status of a job on the job-worker server
  stop        Stop a job on the job-worker server

Flags:
  -h, --help   help for job-worker

Use "job-worker [command] --help" for more information about a command.
```

#### serve

```
Start the job-worker server and listen for connections

Usage:
  job-worker serve [flags]

Flags:
  -h, --help                        help for serve
      --listen-addr string          listen address (default ":8000")
      --max-cpu string              cpu.max value to set in cgroup for each job
      --max-io string               io.max value to set in cgroup for each job
      --max-memory string           memory.max value to set in cgroup for each job
      --shutdown-timeout duration   time to wait for connections to close before forcing shutdown (default 30s)
      --tls-ca-cert string          tls ca cert file to use for validating client certificates (required)
      --tls-cert string             tls server certificate file (required)
      --tls-key string              tls server key file (required)
```

#### child

This is a "hidden" command that the server calls when it reexecutes itself in the new namespace before executing a job. It takes the same flags as `serve` but the additional arguments are the command and args for the job that is going to be executed.

#### start

```
Start a job on the job-worker server

Usage:
  job-worker start [flags] -- command [args]...

Flags:
      --addr string          server address (default ":8000")
  -h, --help                 help for start
      --tls-ca-cert string   tls ca cert file to use for validating server certificate
      --tls-cert string      tls client certificate file (required)
      --tls-key string       tls client key file (required)
```

#### stop

```
Stop a job on the job-worker server

Usage:
  job-worker stop [flags] job-id

Flags:
      --addr string          server address (default ":8000")
  -h, --help                 help for stop
      --tls-ca-cert string   tls ca cert file to use for validating server certificate
      --tls-cert string      tls client certificate file (required)
      --tls-key string       tls client key file (required)
```

#### status

```
Get the status of a job on the job-worker server

Usage:
  job-worker status [flags] job-id

Flags:
      --addr string          server address (default ":8000")
  -h, --help                 help for status
      --tls-ca-cert string   tls ca cert file to use for validating server certificate
      --tls-cert string      tls client certificate file (required)
      --tls-key string       tls client key file (required)
```

#### output

```
Stream the output of a job on the job-worker server

Usage:
  job-worker output [flags] job-id

Flags:
      --addr string          server address (default ":8000")
  -h, --help                 help for output
      --tls-ca-cert string   tls ca cert file to use for validating server certificate
      --tls-cert string      tls client certificate file (required)
      --tls-key string       tls client key file (required)
```
