// Copyright 2022 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package azblob

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/google/uuid"
	"github.com/mendersoftware/deployments/model"
	"github.com/mendersoftware/deployments/storage"
	"github.com/stretchr/testify/assert"
)

var (
	TEST_AZURE_CONNECTION_STRING = flag.String(
		"azure-connection-string",
		os.Getenv("TEST_AZURE_CONNECTION_STRING"),
		"Connection string for azure tests (env: TEST_AZURE_CONNECTION_STRING)",
	)
	TEST_AZURE_CONTAINER_NAME = flag.String(
		"azure-container-name",
		os.Getenv("TEST_AZURE_CONTAINER_NAME"),
		"Container name for azblob tests (env: TEST_AZURE_CONTAINER_NAME)",
	)
	TEST_AZURE_STORAGE_ACCOUNT_NAME = flag.String(
		"azure-account-name",
		os.Getenv("TEST_AZURE_STORAGE_ACCOUNT_NAME"),
		"The storage account name to use for testing "+
			"(env: TEST_AZURE_STORAGE_ACCOUNT_NAME)",
	)
	TEST_AZURE_STORAGE_ACCOUNT_KEY = flag.String(
		"azure-account-key",
		os.Getenv("TEST_AZURE_STORAGE_ACCOUNT_KEY"),
		"The storage account key to use for testing "+
			"(env: TEST_AZURE_STORAGE_ACCOUNT_KEY)",
	)
)

var (
	azureOptions    *Options
	storageSettings = &model.StorageSettings{
		Type:   model.StorageTypeAzure,
		Bucket: *TEST_AZURE_CONTAINER_NAME,
	}
)

func initOptions() {
	opts := NewOptions().
		SetFilenameSuffix(".mender").
		SetContentType("vnd/testing").
		SetBufferSize(BufferSizeMin)
	if *TEST_AZURE_CONTAINER_NAME == "" {
		return
	} else if *TEST_AZURE_CONNECTION_STRING != "" {
		opts.SetConnectionString(*TEST_AZURE_CONNECTION_STRING)
		storageSettings.ConnectionString = TEST_AZURE_CONNECTION_STRING
	} else if *TEST_AZURE_STORAGE_ACCOUNT_NAME != "" && *TEST_AZURE_STORAGE_ACCOUNT_KEY != "" {
		opts.SetSharedKey(SharedKeyCredentials{
			AccountName: *TEST_AZURE_STORAGE_ACCOUNT_NAME,
			AccountKey:  *TEST_AZURE_STORAGE_ACCOUNT_KEY,
		})
		storageSettings.Key = *TEST_AZURE_STORAGE_ACCOUNT_NAME
		storageSettings.Secret = *TEST_AZURE_STORAGE_ACCOUNT_KEY
	} else {
		return
	}
	azureOptions = opts
}

func TestMain(m *testing.M) {
	flag.Parse()
	initOptions()
	ec := m.Run()
	os.Exit(ec)
}

func TestObjectStorage(t *testing.T) {
	if azureOptions == nil {
		t.Skip("Requires env variables TEST_AZURE_CONTAINER_NAME and " +
			"either TEST_AZURE_CONNECTION_STRING or " +
			"TEST_AZURE_STORAGE_ACCOUNT_NAME and TEST_AZURE_STORAGE_ACCOUNT_KEY")
	}
	const (
		blobContent = `foobarbaz`
	)
	var (
		azClient *azblob.ContainerClient
		err      error
	)
	if *TEST_AZURE_CONNECTION_STRING != "" {
		azClient, err = azblob.NewContainerClientFromConnectionString(*TEST_AZURE_CONNECTION_STRING, *TEST_AZURE_CONTAINER_NAME, &azblob.ClientOptions{})
	} else {
		creds := SharedKeyCredentials{
			AccountName: *TEST_AZURE_STORAGE_ACCOUNT_NAME,
			AccountKey:  *TEST_AZURE_STORAGE_ACCOUNT_KEY,
		}
		url, azCred, err := creds.azParams(*TEST_AZURE_CONTAINER_NAME)
		if err != nil {
			t.Fatalf("error initializing blob credential parameters: %s", err)
			return
		}
		azClient, err = azblob.NewContainerClientWithSharedKey(url, azCred, &azblob.ClientOptions{})
	}
	if err != nil {
		t.Fatalf("error initializing blob client: %s", err)
		return
	}
	pathPrefix := "test_" + uuid.NewString() + "/"
	t.Cleanup(func() {
		ctx := context.Background()
		cur := azClient.ListBlobsFlat(&azblob.ContainerListBlobsFlatOptions{
			Prefix: &pathPrefix,
		})
		for cur.NextPage(ctx) {
			rsp := cur.PageResponse()
			if rsp.Segment != nil {
				for _, item := range rsp.Segment.BlobItems {
					if item.Name != nil {
						bc, err := azClient.NewBlobClient(*item.Name)
						if err == nil {
							_, err = bc.Delete(ctx, &azblob.BlobDeleteOptions{})
						}
						if err != nil {
							t.Logf("Failed to delete blob %s: %s", *item.Name, err)
						}
					}
				}
			}
		}
		if err := cur.Err(); err != nil {
			t.Log("ERROR: Failed to clean up testing data:", err)
		}
	})
	testCases := []struct {
		Name string

		CTX    context.Context
		Client storage.ObjectStorage
	}{{
		Name: "default client",

		CTX: context.Background(),
		Client: func() storage.ObjectStorage {
			c, err := New(context.Background(), *TEST_AZURE_CONTAINER_NAME, azureOptions)
			if err != nil {
				t.Fatalf("failed to initialize test case client: %s", err)
			}
			return c
		}(),
	}, {
		Name: "client from context",

		CTX: storage.SettingsWithContext(context.Background(), storageSettings),
		Client: func() storage.ObjectStorage {
			c, err := New(context.Background(), "")
			if err != nil {
				t.Fatalf("failed to initialize test case client: %s", err)
			}
			return c
		}(),
	}}
	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.Name, func(t *testing.T) {
			c := tc.Client
			ctx := tc.CTX
			subPrefix := path.Join(pathPrefix, t.Name())
			err = c.PutObject(ctx, subPrefix+"foo", strings.NewReader(blobContent))
			assert.NoError(t, err)

			stat, err := c.StatObject(ctx, subPrefix+"foo")
			if assert.NoError(t, err) {
				assert.WithinDuration(t, time.Now(), *stat.LastModified, time.Second*10,
					"StatObject; last modified timestamp is not close to present time")
			}

			client := new(http.Client)

			// Test signed requests

			// Generate signed URL for object that does not exist
			_, err = c.GetRequest(ctx, subPrefix+"not_found", time.Minute)
			assert.ErrorIs(t, err, storage.ErrObjectNotFound)

			link, err := c.GetRequest(ctx, subPrefix+"foo", time.Minute)
			if assert.NoError(t, err) {
				req, err := http.NewRequest(link.Method, link.Uri, nil)
				if assert.NoError(t, err) {

					rsp, err := client.Do(req)
					assert.NoError(t, err)
					b, err := io.ReadAll(rsp.Body)
					assert.NoError(t, err)
					_ = rsp.Body.Close()
					assert.Equal(t, blobContent, string(b))
				}
			}

			link, err = c.DeleteRequest(ctx, subPrefix+"foo", time.Minute)
			if assert.NoError(t, err) {
				req, err := http.NewRequest(link.Method, link.Uri, nil)
				if assert.NoError(t, err) {
					rsp, err := client.Do(req)
					if assert.NoError(t, err) {
						assert.Equal(t, http.StatusAccepted, rsp.StatusCode)
						_, err = c.StatObject(ctx, subPrefix+"foo")
						assert.ErrorIs(t, err, storage.ErrObjectNotFound)
					}
				}
			}

			link, err = c.PutRequest(ctx, subPrefix+"bar", time.Minute*5)
			if assert.NoError(t, err) {
				req, err := http.NewRequest(link.Method, link.Uri, strings.NewReader(blobContent))
				if assert.NoError(t, err) {
					for key, value := range link.Header {
						req.Header.Set(key, value)
					}
					rsp, err := client.Do(req)
					if assert.NoError(t, err) {
						assert.Equal(t, http.StatusCreated, rsp.StatusCode)
						stat, err = c.StatObject(ctx, subPrefix+"bar")
						if assert.NoError(t, err) {
							assert.Equal(t, int64(len(blobContent)), *stat.Size)
						}

					}
				}
			}

			err = c.DeleteObject(ctx, subPrefix+"baz")
			assert.ErrorIs(t, err, storage.ErrObjectNotFound)
			assert.Contains(t, err.Error(), storage.ErrObjectNotFound.Error())

			err = c.PutObject(ctx, subPrefix+"baz", strings.NewReader(blobContent))
			if assert.NoError(t, err) {
				err = c.DeleteObject(ctx, subPrefix+"baz")
				assert.NoError(t, err)
			}
		})
	}

}
