// Copyright 2021 Northern.tech AS
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

package mongo

import (
	"context"
	"testing"

	"github.com/mendersoftware/go-lib-micro/identity"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendersoftware/deployments/model"
)

func TestImagesStorageImageByNameAndDeviceType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestDeploymentStorageImageByNameAndDeviceType in short mode.")
	}
	newID := func() string {
		return uuid.NewV4().String()
	}

	//image dataset - common for all cases
	inputImgs := []*model.Image{
		{
			Id: newID(),
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App1 v1.0",
				DeviceTypesCompatible: []string{"foo"},
				Updates:               []model.Update{},
			},
		},
		{
			Id: newID(),
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App2 v0.1",
				DeviceTypesCompatible: []string{"bar", "baz"},
				Updates:               []model.Update{},
			},
		},
	}

	//setup db - common for all cases
	ctx := context.Background()
	db.Wipe()
	client := db.Client()
	store := NewDataStoreMongoWithClient(client)

	for _, image := range inputImgs {
		err := store.InsertImage(ctx, image)
		assert.NoError(t, err)
		if err != nil {
			assert.Fail(t, "error setting up image collection")
		}

		// Convert Depends["device_type"] to bson.A for the sake of
		// simplifying test case definitions.
		image.ArtifactMeta.Depends = make(map[string]interface{})
		image.ArtifactMeta.Depends["device_type"] = make(bson.A,
			len(image.ArtifactMeta.DeviceTypesCompatible),
		)
		for i, devType := range image.ArtifactMeta.DeviceTypesCompatible {
			image.ArtifactMeta.Depends["device_type"].(bson.A)[i] = devType
		}
	}

	testCases := map[string]struct {
		InputImageName string
		InputDevType   string
		InputTenant    string

		OutputImage *model.Image
		OutputError error
	}{
		"name and dev type ok - single type": {
			InputImageName: "App1 v1.0",
			InputDevType:   "foo",

			OutputImage: inputImgs[0],
			OutputError: nil,
		},
		"name and dev type ok - multiple types": {
			InputImageName: "App2 v0.1",
			InputDevType:   "bar",

			OutputImage: inputImgs[1],
			OutputError: nil,
		},
		"name ok, dev type incompatible - single type": {
			InputImageName: "App1 v1.0",
			InputDevType:   "baz",

			OutputImage: nil,
			OutputError: nil,
		},
		"name ok, dev type incompatible - multiple types": {
			InputImageName: "App2 v0.1",
			InputDevType:   "foo",

			OutputImage: nil,
			OutputError: nil,
		},
		"name not found, dev type not found": {
			InputImageName: "App3 v0.1",
			InputDevType:   "bah",

			OutputImage: nil,
			OutputError: nil,
		},
		"name validation error": {
			InputImageName: "",
			InputDevType:   "foo",

			OutputImage: nil,
			OutputError: ErrImagesStorageInvalidArtifactName,
		},
		"dev type validation error": {
			InputImageName: "App2 v0.1",
			InputDevType:   "",

			OutputImage: nil,
			OutputError: ErrImagesStorageInvalidDeviceType,
		},
		"other tenant": {
			InputImageName: "App1 v1.0",
			InputDevType:   "foo",
			InputTenant:    "acme",

			OutputImage: nil,
			OutputError: nil,
		},
	}

	for name, tc := range testCases {

		// Run each test case as subtest
		t.Run(name, func(t *testing.T) {

			if tc.InputTenant != "" {
				ctx = identity.WithContext(ctx, &identity.Identity{
					Tenant: tc.InputTenant,
				})
			} else {
				ctx = context.Background()
			}
			img, err := store.ImageByNameAndDeviceType(ctx,
				tc.InputImageName, tc.InputDevType)

			if tc.OutputError != nil {
				assert.EqualError(t, err, tc.OutputError.Error())
			} else {
				assert.NoError(t, err)

				if tc.OutputImage == nil {
					assert.Nil(t, img)
				} else {
					assert.NotNil(t, img)
					assert.Equal(t, *tc.OutputImage, *img)
				}
			}
		})
	}
}

func TestIsArtifactUnique(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestIsArtifactUnique in short mode.")
	}
	newID := func() string {
		return uuid.NewV4().String()
	}

	//image dataset - common for all cases
	inputImgs := []interface{}{
		&model.Image{
			Id: newID(),
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "app1-v1.0",
				DeviceTypesCompatible: []string{"foo", "bar"},
				Updates:               []model.Update{},
			},
		},
		&model.Image{
			Id: newID(),
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "app2-v2.0",
				DeviceTypesCompatible: []string{"baz", "bax"},
				Updates:               []model.Update{},
				Depends: bson.M{
					"extra": []interface{}{"1", "2"},
				},
			},
		},
	}

	//setup db - common for all cases
	ctx := context.Background()
	db.Wipe()
	client := db.Client()

	collection := client.Database(DatabaseName).Collection(CollectionImages)
	_, err := collection.InsertMany(ctx, inputImgs)
	assert.NoError(t, err)

	testCases := map[string]struct {
		InputArtifactName string
		InputDevTypes     []string
		InputTenant       string

		OutputIsUnique bool
		OutputError    error
	}{
		"artifact unique - unique name": {
			InputArtifactName: "app1-v2.0",
			InputDevTypes:     []string{"foo", "bar"},

			OutputIsUnique: true,
			OutputError:    nil,
		},
		"artifact unique - unique platform": {
			InputArtifactName: "app1-v1.0",
			InputDevTypes:     []string{"baz"},

			OutputIsUnique: true,
			OutputError:    nil,
		},
		"artifact not unique": {
			InputArtifactName: "app1-v1.0",
			InputDevTypes:     []string{"foo", "baz"},

			OutputIsUnique: false,
			OutputError:    nil,
		},
		"empty artifact name": {
			InputDevTypes: []string{"baz", "bah"},

			OutputError: ErrImagesStorageInvalidArtifactName,
		},
		"other tenant": {
			// is unique because we're using another DB
			InputArtifactName: "app1-v1.0",
			InputDevTypes:     []string{"foo", "bar"},
			InputTenant:       "acme",

			OutputIsUnique: true,
		},
	}

	for name, tc := range testCases {

		// Run test cases as subtests
		t.Run(name, func(t *testing.T) {

			if tc.InputTenant != "" {
				ctx = identity.WithContext(ctx, &identity.Identity{
					Tenant: tc.InputTenant,
				})
			} else {
				ctx = context.Background()
			}
			store := NewDataStoreMongoWithClient(client)
			isUnique, err := store.IsArtifactUnique(ctx,
				tc.InputArtifactName, tc.InputDevTypes)

			if tc.OutputError != nil {
				assert.EqualError(t, err, tc.OutputError.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.OutputIsUnique, isUnique)
			}
		})
	}

}

func TestArtifactUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestArtifactUpdate in short mode.")
	}

	//image dataset - common for all cases
	img := &model.Image{
		Id: "a3719bc6-62af-4d65-b781-effa992048ba",
		ImageMeta: &model.ImageMeta{
			Description: "description",
		},

		ArtifactMeta: &model.ArtifactMeta{
			Name:                  "app1-v1.0",
			DeviceTypesCompatible: []string{"foo", "bar"},
			Updates:               []model.Update{},
		},
	}

	//setup db - common for all cases
	ctx := context.Background()
	db.Wipe()
	client := db.Client()

	collection := client.Database(DatabaseName).Collection(CollectionImages)
	_, err := collection.InsertOne(ctx, img)
	assert.NoError(t, err)

	store := NewDataStoreMongoWithClient(client)

	img.ImageMeta.Description = "updated description"
	done, err := store.Update(ctx, img)
	assert.NoError(t, err)
	assert.True(t, done)

	imgFromDB, err := store.ImageByNameAndDeviceType(ctx,
		img.ArtifactMeta.Name,
		img.ArtifactMeta.DeviceTypesCompatible[0])
	assert.NoError(t, err)
	assert.Equal(t, img.ImageMeta.Description, imgFromDB.ImageMeta.Description)
}

func TestListImages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestListImages in short mode.")
	}

	// Make sure we start test with empty database
	db.Wipe()

	inputImgs := []*model.Image{
		{
			Id: "6d4f6e27-c3bb-438c-ad9c-d9de30e59d80",
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App1 v1.0",
				DeviceTypesCompatible: []string{"foo"},
				Updates:               []model.Update{},
			},
		},
		{
			Id: "6d4f6e27-c3bb-438c-ad9c-d9de30e59d81",
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App2 v0.1",
				DeviceTypesCompatible: []string{"foo"},
				Updates:               []model.Update{},
			},
		},
		{
			Id: "6d4f6e27-c3bb-438c-ad9c-d9de30e59d82",
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App1 v1.0",
				DeviceTypesCompatible: []string{"bar, baz"},
				Updates:               []model.Update{},
			},
		},
		{
			Id: "6d4f6e27-c3bb-438c-ad9c-d9de30e59d83",
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App1 v1.0",
				DeviceTypesCompatible: []string{"bork"},
				Updates:               []model.Update{},
			},
		},
		{
			Id: "6d4f6e27-c3bb-438c-ad9c-d9de30e59d84",
			ImageMeta: &model.ImageMeta{
				Description: "description",
			},

			ArtifactMeta: &model.ArtifactMeta{
				Name:                  "App2 v0.1",
				DeviceTypesCompatible: []string{"bar", "baz"},
				Updates:               []model.Update{},
			},
		},
	}

	// setup test context
	ctx := context.Background()
	ds := NewDataStoreMongoWithClient(db.Client())
	for _, img := range inputImgs {
		err := ds.InsertImage(ctx, img)
		assert.NoError(t, err)
		if err != nil {
			assert.FailNow(t, "error setting up image collection for testing")
		}

		// Convert Depends["device_type"] to bson.A for the sake of
		// simplifying test case definitions.
		img.ArtifactMeta.Depends = make(map[string]interface{})
		img.ArtifactMeta.Depends["device_type"] = make(bson.A,
			len(img.ArtifactMeta.DeviceTypesCompatible),
		)
		for i, devType := range img.ArtifactMeta.DeviceTypesCompatible {
			img.ArtifactMeta.Depends["device_type"].(bson.A)[i] = devType
		}
	}

	testCases := map[string]struct {
		filter *model.ReleaseOrImageFilter

		images      []*model.Image
		imagesCount int
		err         error
	}{
		"ok, all": {
			filter: &model.ReleaseOrImageFilter{
				Name: "App",
			},
			images: []*model.Image{
				inputImgs[0],
				inputImgs[2],
				inputImgs[3],
				inputImgs[1],
				inputImgs[4],
			},
			imagesCount: 5,
		},
		"ok, by device type": {
			filter: &model.ReleaseOrImageFilter{
				DeviceType: "bork",
			},
			images: []*model.Image{
				inputImgs[3],
			},
			imagesCount: 1,
		},
		"ok, by name": {
			filter: &model.ReleaseOrImageFilter{
				Name: "App2 v0.1",
			},
			images: []*model.Image{
				inputImgs[1],
				inputImgs[4],
			},
			imagesCount: 2,
		},
		"ok, by name desc, page 2": {
			filter: &model.ReleaseOrImageFilter{
				Name:    "App2 v0.1",
				Sort:    "name:desc",
				Page:    2,
				PerPage: 1,
			},
			images: []*model.Image{
				inputImgs[1],
			},
			imagesCount: 2,
		},
		"ok, not found": {
			filter: &model.ReleaseOrImageFilter{
				Name: "App3 v1.0",
			},
			images:      nil,
			imagesCount: 0,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			images, count, err := ds.ListImages(ctx, tc.filter)

			if tc.err != nil {
				assert.EqualError(t, tc.err, err.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.images, images)
			assert.Equal(t, tc.imagesCount, count)
		})
	}
}
