[![Lint](https://github.com/samirtahir91/github-app-operator/actions/workflows/lint.yml/badge.svg)](https://github.com/samirtahir91/github-app-operator/actions/workflows/lint.yml)
[![Unit tests](https://github.com/samirtahir91/github-app-operator/actions/workflows/tests.yaml/badge.svg)](https://github.com/samirtahir91/github-app-operator/actions/workflows/tests.yaml)
[![Coverage Status](https://coveralls.io/repos/github/samirtahir91/github-app-operator/badge.svg?branch=main)](https://coveralls.io/github/samirtahir91/github-app-operator?branch=main)
[![Build and push](https://github.com/samirtahir91/github-app-operator/actions/workflows/build-and-push.yaml/badge.svg)](https://github.com/samirtahir91/github-app-operator/actions/workflows/build-and-push.yaml)
[![Trivy image scan](https://github.com/samirtahir91/github-app-operator/actions/workflows/trivy.yml/badge.svg)](https://github.com/samirtahir91/github-app-operator/actions/workflows/trivy.yml)
[![Helm](https://github.com/samirtahir91/github-app-operator/actions/workflows/helm-release.yml/badge.svg)](https://github.com/samirtahir91/github-app-operator/actions/workflows/helm-release.yml)

# github-app-operator
This is a Kubernetes operator that will generate an access token for a GithubApp and store it in a secret to use for authenticated requests to Github as the GithubApp. \
It will reconcile a new access token before expiry (1hr).

## Description
Key features:
- Uses a custom resource `GithubApp` in your destination namespace.
- Reads `appId`, `installId` and either and `privateKeySecret` or `vaultPrivateKey` defined in a `GithubApp` resource and requests an access token from Github for the Github App.
  - It stores the access token in a secret `github-app-access-token-{appId}`
- For pulling a GitHub Apps private key, there are 2 options built-in:
  - Using a Kubernetes secret:
    - Use `privateKeySecret` - refers to an existing secret in the namespace which holds the base64 encoded PEM of the Github App's private key.
    - It expects the field `data.privateKey` in the secret to pull the private key from.
  - Hashicorp Vault (this takes priority over `privateKeySecret` if both are specified):
    - **You must base64 encode your private key before saving in Vault**
      - The operator logic will only accept base64 encoded secrets else it will fail.
    - This will create a short-lived JWT (10mins TTL) via Kubernetes Token Request API, with an audience you define.
    - It will then use the JWT and Vault role you define to authenticate with Vault and pull the secret containing the private key.
    - Configure with the `vaultPrivateKey` block:
      - `spec.vaultPrivateKey.mountPath` - Secret mount path, i.e "secret"
      - `spec.vaultPrivateKey.secretPath` - Secret path i.e. "githubapps/{App ID}"
      - `spec.vaultPrivateKey.secretKey` - Secret key i.e. "privateKey"
    - Configure Kubernetes auth with Vault
    - Define a role and optionally audience, service account, namespace etc bound to the role
    - Configure the environment variables in the controller deployment spec:
      - `VAULT_ROLE` - The role you have bound for Kubernetes auth for the operator
      - `VAULT_ROLE_AUDIENCE` - The audience you have bound in Vault
      - `VAULT_ADDR` - FQDN or your Vault server, i.e. `http://vault.default:8200`
    - Additional Vault env vars can be set i.e. `VAULT_NAMESPACE` for enterprise Vault.
      - See [Vault API](https://pkg.go.dev/github.com/hashicorp/vault/api#pkg-constants)
- Deleting the `GithubApp` object will also delete the access token secret it owns.
- The operator will reconcile an access token for a `GithubApp` when:
    - Modifications are made to the access token secret that is owned by a `GithubApp`.
    - Modifications are made to a `GithubApp` object.
    - The access token secret does not exist or does not have a `status.expiresAt` value
- Periodically the operator will check the expiry time of the access token and reconcile a new access token if the threshold is met or if the access token is invalid (checks against GitHub API).
- It stores the expiry time of the access token in the `status.expiresAt` field of the `GithubApp` object.
- If any errors are recieved during a reconcile they are set in the `status.error` field of the `GithubApp` object.
- It will skip requesting a new access token if the expiry threshold is not reached/exceeded.
- You can override the check interval and expiry threshold using the deployment env vars:
  - `CHECK_INTERVAL` - i.e. to check every 5 mins set the value to `5m`
    - It will default to `5m` if not set
  - `EXPIRY_THRESHOLD` - i.e. to reconcile a new access token if there is less than 10 mins left from expiry, set the value to `10m`
    - It will default to `15m` if not set
- Optionally, you can enable rolling upgrade to deployments in the same namespace as the `GithubApp` that match any of the labels you define in `spec.rolloutDeployment.labels`
  - This is useful where pods need to be recreated to pickup the new secret data.
- By default the logs are json formatted and log level is set to info and error, you can set `DEBUG_LOG` to `true` in the manager deployment environment variable for debug level logs.
- The CRD has additional data printed with `kubectl get`:
  - APP ID
  - INSTALL ID
  - EXPIRES AT
  - ERROR
- Events are recorded for:
  - Any error on reconcile for a GithubApp
  - Creation of an access token secret
  - Updating an access token secret
  - Updating a deployment for rolling upgrade

## Example creating a secret to hold a GitHub App private key
- Get your GithubApp private key and encode to base64
```sh
base64 -w 0 private-key.pem
```
- Create a secret to hold the private key
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-app-secret
  namespace: YOUR_NAMESPACE
type: Opaque
data:
  privateKey: BASE64_ENCODED_PRIVATE_KEY
```
## Example GithubApp object
- Below example will setup a GithubApp and reconcile an access token in the `team-1` namespace, the access token will be available to use in the secret `github-app-access-token-123123`
- It authenticates with GitHub API using your private key secret like above example.
```sh
kubectl apply -f - <<EOF
apiVersion: githubapp.samir.io/v1
kind: GithubApp
metadata:
  name: GithubApp-sample
  namespace: team-1
spec:
  appId: 123123
  installId: 12312312
  privateKeySecret: github-app-secret
EOF
```

## Example GithubApp object with pod restart (deployment rolling upgrade) on token renew
- Below example will upgrade deployments in the `team-1` namespace when the github token is modified, matching any of labels:
  - foo: bar
  - foo2: bar2
```sh
kubectl apply -f - <<EOF
apiVersion: githubapp.samir.io/v1
kind: GithubApp
metadata:
  name: GithubApp-sample
  namespace: team-1
spec:
  appId: 123123
  installId: 12312312
  privateKeySecret: github-app-secret
  rolloutDeployment:
    labels:
      foo: bar
      foo2: bar2
EOF
```

## Example GithubApp object using Vault to pull the private key during run-time
- Below example will request a new JWT from Kubernetes and use it to fetch the private key from Vault when the github access token expires
```sh
kubectl apply -f - <<EOF
apiVersion: githubapp.samir.io/v1
kind: GithubApp
metadata:
  name: GithubApp-sample
  namespace: team-1
spec:
  appId: 123123
  installId: 12312312
  vaultPrivateKey:
    mountPath: secret
    secretPath: githubapp/123123
    secretKey: privateKey
EOF
```

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.22.0+.
- Access to a Kubernetes v1.22.0+ cluster.

### To deploy with Helm using public Docker image
A helm chart is generated using `make helm` when a new tag is pushed, i.e a release.
You can pull the helm chart from this repos packages
- See the [packages page](https://github.com/samirtahir91/github-app-operator/pkgs/container/github-app-operator%2Fhelm-charts%2Fgithub-app-operator)
- Pull with helm:
  - ```sh
    helm pull oci://ghcr.io/samirtahir91/github-app-operator/helm-charts/github-app-operator --version <TAG>
    ```
- Untar the chart and edit the `values.yaml` as required.
- You can use the latest public image on DockerHub - `samirtahir91076/github-app-operator:latest`
  - See [tags](https://hub.docker.com/r/samirtahir91076/github-app-operator/tags) 
- Deploy the chart with Helm.

### To Deploy on the cluster (from source and with Kustomize)
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/github-app-operator:tag
```

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/github-app-operator:tag
```

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

### Testing

Current integration tests cover the scenarios:
- Modifying an access token secret triggers reconcile of new access token.
- Deleting an access token secret triggers reconcile of a new access token secret.
- Reconcile of access token is valid.
- Reconcile error is recorded in a `GithubApp` object's `status.error` field
- The `status.error` field is cleared on succesful reconcile for a `GithubApp` object.
- Deployments are upgraded for rolling upgrade matching a label if defined in `spec.rolloutDeployment.labels` for a `GithubApp` (requires USE_EXISTING_CLUSTER=true).
- Vault integration for private key secret using Kubernetes auth (requires USE_EXISTING_CLUSTER=true).

**Run the controller in the foreground for testing:**
```sh
# PRIVATE_KEY_CACHE_PATH folder to temp store private keys in local file system
# /tmp/github-test is fine for testing
export PRIVATE_KEY_CACHE_PATH=/tmp/github-test/
# run
make run
```

**Run integration tests against a real cluster, i.e. Minikube:**
- Export your GitHub App private key as a `base64` string and then run the tests
```sh
export GITHUB_PRIVATE_KEY=<YOUR_BASE64_ENCODED_GH_APP_PRIVATE_KEY>
export GH_APP_ID=<YOUR GITHUB APP ID>
export GH_INSTALL_ID=<YOUR GITHUB APP INSTALL ID>
export "VAULT_ADDR=http://localhost:8200" # this can be local k8s Vault or some other Vault
export "VAULT_ROLE_AUDIENCE=githubapp"
export "VAULT_ROLE=githubapp"
```
- This uses Vault, you can spin up a simple Vault server using this script.
- It will use Helm and configure the Vault server with a test private key as per the env var ${GITHUB_PRIVATE_KEY}.
```sh
cd scripts
./install_and_setup_vault_k8s.sh
# Run vault port forward
kubectl port-forward vault-0 8200:8200
```
- Run tests
```sh
cd ..
USE_EXISTING_CLUSTER=true make test
```

**Run integration tests using env test (without a real cluster):**
- Export your GitHub App private key as a `base64` string and then run the tests.
- This will skip the Vault and Deployment Rollout test cases.
```sh
export GITHUB_PRIVATE_KEY=<YOUR_BASE64_ENCODED_GH_APP_PRIVATE_KEY>
export GH_APP_ID=<YOUR GITHUB APP ID>
export GH_INSTALL_ID=<YOUR GITHUB APP INSTALL ID>
USE_EXISTING_CLUSTER=false make test
```

**Generate coverage html report:**
```sh
go tool cover -html=cover.out -o coverage.html
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
