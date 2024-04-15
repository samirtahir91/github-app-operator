#!/bin/bash

# Run this script to setup vault on kubernetes with a simple role, policy and k8s auth
# Export your github app private key then run the script
# export GITHUB_PRIVATE_KEY=<YOUR GITHUB APP PRIVATE KEY>

helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update

# install vault with single node
helm install vault hashicorp/vault --values helm-vault-raft-values.yml
kubectl get pods

# wait for vault to run
until kubectl get pod vault-0 -o=jsonpath='{.status.phase}' | grep -q "Running"; do sleep 5; done

# get cluster keys
kubectl exec vault-0 -- vault operator init \
  -key-shares=1 \
  -key-threshold=1 \
  -format=json > cluster-keys.json

# set unseal key
VAULT_UNSEAL_KEY=$(jq -r ".unseal_keys_b64[]" cluster-keys.json)

# unseal vault
kubectl exec vault-0 -- vault operator unseal ${VAULT_UNSEAL_KEY}

# wait for vault to be ready
kubectl wait --for=condition=ready pod/vault-0 --timeout=300s

# get root token
VAULT_ROOT_TOKEN=$(jq -r ".root_token" cluster-keys.json)

# login
kubectl exec -i vault-0 -- vault login -non-interactive ${VAULT_ROOT_TOKEN}

# enable kv-v2
kubectl exec -i vault-0 -- vault secrets enable -path=secret kv-v2

# write github app secret
kubectl exec -i vault-0 -- vault kv put secret/githubapp/test privateKey="${GITHUB_PRIVATE_KEY}"

# enable k8s auth
kubectl exec -i vault-0 -- vault auth enable kubernetes

# get k8s host
KUBERNETES_HOST=$(kubectl exec -i vault-0 -- sh -c 'echo $KUBERNETES_SERVICE_HOST')

# write k8s host
kubectl exec -i vault-0 -- vault write auth/kubernetes/config kubernetes_host="https://$KUBERNETES_HOST:443"

# write vault policy
kubectl exec -i vault-0 -- sh -c 'vault policy write githubapp - <<EOF
path "secret/data/githubapp/test" {
  capabilities = ["read"]
}
EOF'

# write vault role
kubectl exec -i vault-0 -- sh -c 'vault write auth/kubernetes/role/githubapp \
  bound_service_account_names="default" \
  bound_service_account_namespaces="namespace0" \
  policies=githubapp \
  ttl=24h'