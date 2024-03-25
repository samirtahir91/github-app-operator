# github-app-operator
This is a Kubernetes operator that will generate an access token for a GithubApp and store it in a secret to use for authenticated requests to Github as the GithubApp.

## Description
Key features:
- Uses a custom resource `GithubApp` in your destination namespace.
- Reads `appId`, `installId` and `privateKeySecret` defined in a `GithubApp` resource and requests an access token from Github for the Github App.
  - It stores the access token in a secret `github-app-access-token-{appId}`
- The `privateKeySecret` refers to an existing secret in the namespace which holds the base64 encoded PEM of the Github App's private key.
  - It expects the field `data.privateKey` in the secret to pull the private key from.
- Deleting the `GithubApp` object will also delete the access token secret it owns.
- The operator will reconcile an access token for a `GithubApp` when:
    - Modifications are made to the access token secret that is owned by a `GithubApp`.
    - Modifications are made to a `GithubApp` object.
    - The access token secret does not exist or does not have a `status.expiresAt` value
- Periodically the operator will check the expiry time of the access token and reconcile a new access token if the threshold is met.
- It stores the expiry time of the access token in the `status.expiresAt` field of the `GithubApp` object.
- It will skip requesting a new access token if the expiry threshold is not reached/exceeded.
- You can set override the check interval and expiry threshold using the deployment env vars:
  - `CHECK_INTERVAL` - i.e. to check every 5 mins set the value to `5`
    - It will default to `5` if not set
  - `EXPIRY_THRESHOLD` - i.e. to reconcile a new access token if there is less than 10 mins left from expiry, set the value to `10`
    - It will default to `15` if not set

## Example creating a secret to hold a GitHub App private key
- Get your GithubApp private key and encode to base64
```sh
base64 -w 0 private-key.pem
```
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
Below example will setup a 
```sh
kubectl apply -f - <<EOF
apiVersion: sync.samir.io/v1
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

## Getting Started

### Prerequisites
- go version v1.22.1+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/secret-sync-operator:tag
```

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/secret-sync-operator:tag
```

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

### Testing

Current integration tests cover the scenarios:


**Run the controller in the foreground for testing:**
```sh
make run
```

**Run integration tests:**
```sh
make test
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
