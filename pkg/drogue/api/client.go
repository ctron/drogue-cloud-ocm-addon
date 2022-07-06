package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
)

type Client struct {
	client *http.Client
	url    string
	auth   string
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func New(apiUrl string, username string, token string) (*Client, error) {
	client := &http.Client{}

	auth := "Basic " + basicAuth(username, token)

	return &Client{url: apiUrl, client: client, auth: auth}, nil
}

func (c *Client) ListDevices(application string) ([]Device, error) {
	reqUrl := c.url + "/api/registry/v1alpha1/apps/" + url.PathEscape(application) + "/devices"
	req, err := http.NewRequest("GET", reqUrl, nil)
	req.Header.Add("Authorization", c.auth)

	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var devices []Device
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, err
	}

	return devices, nil
}

func (c *Client) GetDevice(application string, device string) (*Device, error) {
	reqUrl := c.url + "/api/registry/v1alpha1/apps/" + url.PathEscape(application) + "/devices/" + url.PathEscape(device)
	req, err := http.NewRequest("GET", reqUrl, nil)
	req.Header.Add("Authorization", c.auth)

	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		var result Device
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		return &result, nil
	case 404:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown return code: %v", resp.StatusCode)
	}

}

func (c *Client) UpdateDevice(device *Device) error {

	payload, err := json.Marshal(device)
	if err != nil {
		return err
	}

	application := device.Metadata.Application

	reqUrl := c.url + "/api/registry/v1alpha1/apps/" + url.PathEscape(application) + "/devices/" + url.PathEscape(device.Metadata.Name)
	req, err := http.NewRequest("PUT", reqUrl, bytes.NewReader(payload))
	req.Header.Add("Authorization", c.auth)
	req.Header.Add("Content-Type", "application/json")

	if err != nil {
		return err
	}

	klog.Infof("Updating device: %+v:\n%s", req, string(payload))

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("failed to update: %v %v", resp.StatusCode, resp.Status)
	}

	return nil
}
