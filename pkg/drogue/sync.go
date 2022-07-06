package drogue

import (
	"context"
	"encoding/json"
	"fmt"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/types"
	"time"

	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/config"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/drogue/api"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

type DeviceEvent struct {
	Device string

	Data *api.Device
}

// DeviceChangeNotifier Synchronize devices from Drogue Cloud with the ManagedClusterAddOn CR
//
// A change in the CR will trigger a reconciliation, and so a rollout to the device.
type DeviceChangeNotifier struct {
	config      *config.DeviceSynchronizerConfig
	client      *api.Client
	addonClient *addonv1alpha1client.Clientset
}

func NewDeviceSynchronizer(config *config.DeviceSynchronizerConfig, kubeConfig *rest.Config) (*DeviceChangeNotifier, error) {

	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	client, err := api.New(config.API.URL, config.Credentials.Username, config.Credentials.Token)

	if err != nil {
		return nil, err
	}

	return &DeviceChangeNotifier{
		config:      config,
		client:      client,
		addonClient: addonClient,
	}, nil
}

func (s *DeviceChangeNotifier) Start(ctx context.Context) error {

	klog.Infoln("Starting Drogue Cloud MQTT event consumer")
	klog.Infof("Configuration: %+v", s.config)

	opts := mqtt.NewClientOptions()

	opts.AddBroker(s.config.MQTT.IntegrationUrl)
	opts.CleanSession = true
	opts.AutoReconnect = true
	opts.ConnectRetry = true
	opts.ProtocolVersion = 4

	opts.Username = s.config.Credentials.Username
	opts.Password = s.config.Credentials.Token

	events := make(chan string)
	go s.runMqttLoop(ctx, opts, events)
	go s.runSyncLoop(ctx, events)

	return nil
}

func (s *DeviceChangeNotifier) runMqttLoop(ctx context.Context, opts *mqtt.ClientOptions, events chan string) {

	klog.Infoln("Running main loop")
	klog.Infof("Brokers: %v", opts.Servers)

	msgs := make(chan mqtt.Message)

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		klog.Infoln("MQTT connection established. Subscribing to events...")
		topic := fmt.Sprintf("$share/%s/app/%s", s.config.MQTT.Group, s.config.Application)
		token := client.Subscribe(topic, 1, func(client mqtt.Client, message mqtt.Message) {
			msgs <- message
		})
		go func() {
			_ = token.Wait()
			if token.Error() != nil {
				klog.Fatalf("Failed to subscribe to application events: %v", token.Error())
			} else {
				klog.Infoln("MQTT subscription complete")
			}
		}()
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	klog.Infoln("Connect returned, waiting for outcome...")
	token.Wait()
	if token.Error() != nil {
		klog.Warningf("Connect failed: %v", token.Error())
	} else {
		klog.Infoln("Connected")
	}

	for {
		select {
		case <-ctx.Done():
			klog.Infoln("Terminating addon process")
			client.Disconnect(250)
			return
		case msg := <-msgs:
			klog.Infof("Received message: %+v\n", msg)
			if err := s.handleEvent(events, msg); err != nil {
				klog.Warningf("failed to handle event: %v", err)
			}
		}
	}

}

/// handle a cloud event, coming from the application event stream
func (s *DeviceChangeNotifier) handleEvent(events chan string, msg mqtt.Message) error {
	event := cloudevents.NewEvent()
	if err := json.Unmarshal(msg.Payload(), &event); err != nil {
		return err
	}

	if event.Type() != "io.drogue.registry.v1" || event.Subject() != "devices" {
		// wrong event type, silently ignore
		return nil
	}

	// extract device name
	device, err := types.ToString(event.Extensions()["device"])
	if err != nil {
		return err
	}
	if device == "" {
		return fmt.Errorf("missing device name in event")
	}

	// send event
	events <- device

	// done
	return nil
}

func (s *DeviceChangeNotifier) runSyncLoop(ctx context.Context, events chan string) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	klog.Infoln("Initial sync")
	s.syncOnce()

	for {
		select {
		case <-ctx.Done():
			klog.Infoln("Terminating addon process")
			return
		case device := <-events:
			klog.Infof("Device changed: %v\n", device)
			if err := s.performSyncDevice(device); err != nil {
				klog.Warningf("Failed to sync device (%s): %v", device, err)
			}
		case <-ticker.C:
			klog.Infoln("Ticking...")
			s.syncOnce()
		}
	}
}

// syncOnce will iterate through all devices once, and trigger a reconcile process
func (s *DeviceChangeNotifier) syncOnce() {
	klog.Infoln("Sync all devices")

	if err := s.performSyncOnce(); err != nil {
		klog.Warningln("Failed to synchronize all devices", err)
	}

}

// performSyncOnce performs the actual sync process
func (s *DeviceChangeNotifier) performSyncOnce() error {

	devices, err := s.client.ListDevices(s.config.Application)
	if err != nil {
		return err
	}

	for _, device := range devices {
		if err := s.syncDevice(DeviceEvent{
			Device: device.Metadata.Name,
			Data:   &device,
		}); err != nil {
			klog.Warningf("Failed to synchronize device (%s): %v", device.Metadata.Name, err)
		}
	}

	// Devices which have been deleted in Drogue Cloud, but still exist here, are covered by this controller's
	// reconcile loop

	return nil

}

// performSyncDevice perform the synchronization of a single device, notified by the event stream
func (s *DeviceChangeNotifier) performSyncDevice(device string) error {

	data, err := s.client.GetDevice(s.config.Application, device)

	if err != nil {
		return err
	}

	return s.syncDevice(DeviceEvent{
		Device: device,
		Data:   data,
	})

}

// syncDevice triggers the synchronization of a device/cluster, but up-ticking the "observed generation" of the
// addon CR.
func (s *DeviceChangeNotifier) syncDevice(event DeviceEvent) error {

	klog.Infof("Synchronizing device (%s/%s)", s.config.Application, event.Device)

	// The Drogue Cloud device name is the name of the Kubernetes namespace
	ns := event.Device

	// Get the Managed Cluster Addon
	addon, err := s.addonClient.AddonV1alpha1().ManagedClusterAddOns(ns).Get(context.Background(), "drogue-cloud", v1.GetOptions{})
	if err != nil {

		if errors.IsNotFound(err) {

			// cluster addon not found, OK but remove finalizer
			if event.Data != nil {
				if event.Data.CleanupDevice() {
					// information was present, now removed
					if err := s.client.UpdateDevice(event.Data); err != nil {
						klog.Warningf("failed to update device: %v", err)
					}
				}
			}

		} else {
			klog.Warningf("Failed to get managed addon for device: %v", err)
			return err
		}

	} else {

		klog.Infoln("Addon present, uptick observed generation")

		// And make a change, just to trigger the reconcile loop
		if addon.Annotations == nil {
			addon.Annotations = map[string]string{}
		}
		addon.Annotations["drogue-last-change"] = time.Now().Format(time.RFC3339)

		klog.Infof("Addon: %+v", addon)

		// Write back status to trigger the reconcile loop
		_, err = s.addonClient.AddonV1alpha1().ManagedClusterAddOns(ns).Update(context.Background(), addon, v1.UpdateOptions{})
		if err != nil {
			klog.Warningln("Failed to update state of device:", err)
			return err
		}

	}

	// Done
	return nil
}
