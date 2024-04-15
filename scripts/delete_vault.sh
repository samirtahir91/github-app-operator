#!/bin/bash

# Run this script to delete the vault setup in kubernetes

helm delete vault
kubectl delete pvc data-vault-0