+++
title = "Security"
description = "Sign and verify tasks with ed25519 keys"
toc = true
weight = 50
+++

Handlers sometimes run in untrusted locations and must only execute tasks from trusted creators.

Tasks can be signed with ed25519 private keys. Clients can be configured to accept only tasks created and signed with a specific key. When keys are configured, signatures on all tasks are required by default. Clients can also be configured to accept unsigned tasks, while still verifying signatures when present.

Create keys and save them to a file using `hex.Encode()`:

```go
pubk, prik, err = ed25519.GenerateKey(nil)
panicIfErr(err)
```

Configure the client:

```go
client, err := asyncjobs.NewClient(
    asyncjobs.NatsContext("AJC"),
	
    // when tasks are created sign using this ed25519.PrivateKey, see also TaskSigningSeedFile()
    asyncjobs.TaskSigningKey(prik),

    // when loading tasks verify using this ed25519.PublicKey, see also TaskVerificationKeyFile()
    asyncjobs.TaskVerificationKey(pubk),

    // support loading unsigned tasks when a verification method is set, disabled by default
    asyncjobs.TaskSignaturesOptional(),
)
panicIfErr(err)
```

On the command line, `ajc tasks` supports `--sign` and `--verify` flags. Each accepts either a hex-encoded key or a path to a file containing a hex-encoded key.

Docker containers built with `ajc package docker` can set a key via the `AJ_VERIFICATION_KEY` environment variable. Optional signatures can be enabled at build time by setting `task_signatures_optional: true` in `asyncjobs.yaml`.
