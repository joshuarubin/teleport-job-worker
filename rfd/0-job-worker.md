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

The worker library defines a simple interface with methods to start, stop, query status and get the output of a job.
The implementation is responsible for starting jobs, keeping track of their output, status and exit codes.

##### cgroups

The library implementation will be configured at instantiation with limits that can be parsed into values for `cpu.max`, `memory.max` and `io.max`. These values will originate from runtime flags provided to the server with defaults that do not implement limits. All processes will be started in a new cgroup regardless of the values of these limits. Note that for expediency, it is assumed that cgroups v2 are in use on the system running the service.

##### namespaces

The library implementation will be configured such that it knows how to execute the server binary as a child process, with the proper clone and unshare flags to execute in a new pid, mount and network namespace, and call back into the library to execute the given command for the job. This library instance is obviously separate from the main server process. It has several jobs to do though. It has to remount the `/proc` filesystem, create and configure the cgroup, set the cgroup for the pid and exec the job. The library in the main server process will then be able to follow the exit code, `stdout` and `stderr`.

##### Job Output Storage

Output for each job will be maintained, in memory, as long as the worker implementation exists. This poses a potential memory leak, but for the purposes of this challenge, is an acceptable solution. Later designs would likely need to write job output to disk or possibly even remote storage.

##### Job Output Streaming

The worker library streams job output through an `io.ReadCloser`. The server will indicate the end of the stream, if the job has completed, with the `io.EOF` error. The client may, at any time, close the stream to indicate that it is disconnecting. The client must always close the stream when it no longer needs it to prevent memory leaks.

##### Identifiers

JobID is an opaque identifier that is internally mapped to the pid of the process in the host namespace. Since pid is not guaranteed to be unique, nothing internal to the library will be keyed off of the pid. The pid is still required, though, in order to signal the process to stop, for example. The `go.jetify.com/typeid` library is used to generate job ids.

UserID can be any unique value as far as the library is concerned. It is used for authorization such that only the user that starts a job is permitted to stop it, get its status or output. Practically, it is expected to be the serial number of the client certificate used to make the connection to the server. This way the client doesn't have to provide any other kind of identification. The serial number is guaranteed to be unique for all certificates signed by a given authority thus making this a valid method to identify unique users.

#### gRPC API

The gRPC API follows the worker library fairly closely. It does not require the user id in the messages because user id is derived from the client certificate used to make the connection. Additionally, the stream output is different since gRPC can't stream bytes, rather, it will stream one newline separated string at a time. Last, it has no need to be able to execute the job in a child process as that's only required by the library.

### Security Considerations

```go
cfg := &tls.Config{
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    clientCAs,
    Certificates: []tls.Certificate{crt},
    MinVersion:   tls.VersionTLS13,
}
```

### CLI UX

### Implementation Details
