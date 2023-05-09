+++
title = "Security"
toc = true
weight = 50
+++

Sometimes you want to run a handler in a insecure location and want to be sure it only executes tasks from trusted creators.

Tasks can be signed using ed25519 private keys and clients can be configured to only accept tasks created and signed using
a specific key.  We support requiring all tasks are signed when keys are configured (the default), or accepting unsigned tasks
but requiring signed tasks are verified.

First we need to create some keys, these should be saved to a file encoded using `hex.Encode()`.

```go
pubk, prik, err = ed25519.GenerateKey(nil)
panicIfErr(err)
```

Then we can configure the client:

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

On the command line the `ajc tasks` command has `--sign` and `--verify` flags which can either be hex encoded keys
or paths to files holding them in hex encoded format.

Docker containers built using `ajc package docker` can set a key in the environment variable `AJ_VERIFICATION_KEY` and
can opt into optional signatures at build time by setting `task_signatures_optional: true` in the `asyncjobs.yaml`.