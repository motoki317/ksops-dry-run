[![License][license-badge]][license-link]

# KSOPS Dry Run

🔓 Kustomize plugin to fake the decryption of ksops secrets

## Motivations

> ⚠️ This repository is intended to be deprecated if something like [mozilla/sops #1015](https://github.com/mozilla/sops/issues/1015) is ever implemented.

The [ksops](https://github.com/viaduct-ai/kustomize-sops) plugin is fantastic for managing encrypted secret resources as part of a Kustomize application.
One specific limitation is the inability to run `kustomize build` against any application that contains secrets that you do not have access to.
Since you do not have sufficient access to decrypt those secrets, the ksops plugin fails, and by extension so does the entire `kustomize build` invocation.

This comes up commonly in cases such as:
- A developer not being able to validate changes to their application in a production configuration.
- A CI pipeline not being able to validate every application across every environment.

## How it works

This repo provides a kustomize plugin that solves the above problems.

This plugin operates by subverting the job of the original `ksops` plugin.
It acts identically to the original `ksops` plugin, but instead of producing decrypted secret resources, it instead produces secret resources where the (formerly encrypted) values are replaced with a placeholder value.  
This way you can run `kustomize build` and produce resource manifests for your application without actually needing to decrypt them.

## Installation

To install, download the `ksops-dry-run` plugin rename to `ksops` and place it in PATH.

```shell
$ wget https://github.com/joshdk/ksops-dry-run/releases/download/v0.2.0/ksops-dry-run-linux-amd64.tar.gz
$ tar -xf ksops-dry-run-linux-amd64.tar.gz
$ mv ksops-dry-run ksops
```

## Usage

Take for example, a simple kustomize app containing a single ksops encrypted secret:

```shell
$ ls

kustomization.yaml
secret-generator.yaml
secret.enc.yaml
```

We can view that `secret.enc.yaml` file has been encrypted.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: example-secret

stringData:
  SECRET_TOKEN: ENC[AES256_GCM,data:...type:str]

sops:
  kms:
    ...
```

And referenced from `secret-generator.yaml` and `kustomization.yaml`.

```yaml
apiVersion: viaduct.ai/v1
kind: ksops
metadata:
  name: ksops
  annotations:
    config.kubernetes.io/function: |
      exec:
        path: ksops

files:
  - ./secret.enc.yaml
```

```yaml
generators:
  - secret-generator.yaml
```

If we run `kustomize build --enable-alpha-plugins --enable-exec .` before installing `ksops-dry-run`, we can see that our secret is decrypted normally (potentially requiring a gpg key or other KMS API credentials):

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: example-secret
stringData:
  SECRET_TOKEN: s00per_s3cret_t0k3n
```

But if instead we run `kustomize build --enable-alpha-plugins --enable-exec .` with `ksops-dry-run` installed, we can see that our secret has been stubbed out with placeholder values, all without performing any actual decryption:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: example-secret
stringData:
  SECRET_TOKEN: KSOPS_DRY_RUN_PLACEHOLDER
```

## License

This code is distributed under the [MIT License][license-link], see [LICENSE.txt][license-file] for more information.

[license-badge]:         https://img.shields.io/badge/license-MIT-green.svg
[license-file]:          https://github.com/joshdk/ksops-dry-run/blob/master/LICENSE.txt
[license-link]:          https://opensource.org/licenses/MIT
