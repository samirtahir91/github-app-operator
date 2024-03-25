# github-app-operator

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

## Description
Key features:


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