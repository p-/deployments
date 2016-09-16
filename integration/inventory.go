// Copyright 2016 Mender Software AS
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

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/pkg/errors"
)

// Routes
const (
	DevicesInventory string = "/api/integrations/0.1/inventory/devices/%s"
)

type Attibute struct {
	Name        string      `json:"name" valid:"length(1|4096),required"`
	Description string      `json:"description" valid:"optional"`
	Value       interface{} `json:"value" valid:"length(1|4096),required`
}

type Device struct {
	ID         DeviceID    `json:"id" valid:"length(1|4096),required"`
	Updated    time.Time   `json:"updated_ts" valid:"required"`
	Attributes []*Attibute `json:"Attributes" valid:"optional"`
}

func (d *Device) Validate() error {
	_, err := govalidator.ValidateStruct(d)
	return err
}

type DeviceID string

func (d DeviceID) String() string {
	return string(d)
}

type Inventory interface {
	// Fetch Device object from inventory service.
	GetDeviceInventory(id DeviceID) (*Device, error)
}

// GetDeviceInventory returns device object from inventory
// If object is not found return nil, nil
func (api *MenderAPI) GetDeviceInventory(id DeviceID) (*Device, error) {

	resp, err := api.client.Get(fmt.Sprintf(api.uri+DevicesInventory, id))
	if err != nil {
		return nil, errors.Wrap(err, "sending request for device inventory")
	}

	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		return nil, errors.Wrap(api.parseErrorResponse(resp.Body), "error server response")
	}

	device := Device{}
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return nil, errors.Wrap(err, "parsig server response")
	}

	if err := (&device).Validate(); err != nil {
		return nil, errors.Wrap(err, "validating server response")
	}

	return &device, nil
}
