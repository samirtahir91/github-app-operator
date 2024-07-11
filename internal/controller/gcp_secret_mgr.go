/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/crc32"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// GetSecretFromGcp accesses the payload for the given secret version if one
// exists. The version can be a version number as a string (e.g. "5") or an
// alias (e.g. "latest").
func (r *GithubAppReconciler) GetSecretFromSecretMgr(name string) ([]byte, error) {

	// Create the client.
	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to create secretmanager client: %w", err)
	}

	// Defer closing the client and check for errors
	defer func() {
		err := client.Close()
		if err != nil {
			fmt.Printf("error closing client for secret manager: %v\n", err)
		}
	}()

	// Build the request.
	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	}

	// Call the API.
	result, err := client.AccessSecretVersion(ctx, req)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to access secret version: %w", err)
	}

	// Verify the data checksum.
	crc32c := crc32.MakeTable(crc32.Castagnoli)
	checksum := int64(crc32.Checksum(result.Payload.Data, crc32c))
	if checksum != *result.Payload.DataCrc32C {
		return []byte(""), fmt.Errorf("data corruption detected")
	}

	privateKeyStr := string(result.Payload.Data)

	// Base64 decode the private key
	// The private key must be stored as a base64 encoded string in the gcp secret manager secret
	privateKey, err := base64.StdEncoding.DecodeString(privateKeyStr)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to base64 decode the gcp secret managed private key: %v", err)
	}

	return privateKey, nil
}
