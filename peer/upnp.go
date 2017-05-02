package peer

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

type igdClient interface {
	GetExternalIPAddress() (string, error)
	AddPortMapping(externalAddr string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseDuration uint32) error
	DeletePortMapping(externalAddr string, externalPort uint16, protocol string) error
	GetServiceClient() *goupnp.ServiceClient
}

type igdDiscoveryResponse struct {
	Devices []igdClient
	Error   error
}

type IGD struct {
	client igdClient
}

type PortMapping struct {
	ExternalPort    int
	InternalAddress string
	InternalPort    int
	Protocol        string
	Description     string
	LeaseDuration   time.Duration
	IGD             *IGD
}

func (self *PortMapping) Delete() error {
	if err := self.IGD.DeletePortMapping(self); err != nil {
		return err
	}

	return nil
}

func (self *PortMapping) String() string {
	return fmt.Sprintf("%s:%d/%s",
		self.InternalAddress,
		self.InternalPort,
		strings.ToLower(self.Protocol))
}

func DiscoverGateways(timeout time.Duration) ([]*IGD, error) {
	discochan := make(chan igdDiscoveryResponse)
	devices := make([]*IGD, 0)

	go func() {
		igdDevices := make([]igdClient, 0)
		var lastError error

		// IGD2 discovery
		if ig2, _, err := internetgateway2.NewWANIPConnection1Clients(); err == nil {
			for _, ig := range ig2 {
				var i igdClient
				i = ig
				igdDevices = append(igdDevices, i)
			}
		} else {
			lastError = err
		}

		// IGD1 discovery
		if ig1, _, err := internetgateway1.NewWANIPConnection1Clients(); err == nil {
			for _, ig := range ig1 {
				var i igdClient
				i = ig
				igdDevices = append(igdDevices, i)
			}
		} else {
			lastError = err
		}

		for _, device := range igdDevices {
			log.Debugf("Found gateway device: %s", device.GetServiceClient().RootDevice.URLBase.Host)
		}

		discochan <- igdDiscoveryResponse{
			Devices: igdDevices,
			Error:   lastError,
		}
	}()

	// retrieve all discovered gateway devices
	select {
	case discovery := <-discochan:
		if discovery.Error == nil {
			for _, igdDevice := range discovery.Devices {
				devices = append(devices, &IGD{
					client: igdDevice,
				})
			}
		} else {
			return nil, discovery.Error
		}
	case <-time.After(timeout):
		return nil, fmt.Errorf("UPnP device discovery timed out after %s", timeout)
	}

	return devices, nil
}

func (self *IGD) AddPortMapping(protocol string, internalPort int, externalPort int, description string, leaseDuration time.Duration) (*PortMapping, error) {
	if localAddr, err := self.GetLocalIP(); err == nil {
		if err := self.client.AddPortMapping(
			``,
			uint16(externalPort),
			protocol,
			uint16(internalPort),
			localAddr,
			true,
			description,
			uint32(leaseDuration/time.Second)); err == nil {

			return &PortMapping{
				IGD:             self,
				ExternalPort:    externalPort,
				InternalAddress: localAddr,
				InternalPort:    internalPort,
				Protocol:        protocol,
				Description:     description,
				LeaseDuration:   leaseDuration,
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *IGD) DeletePortMapping(mapping *PortMapping) error {
	if mapping != nil && mapping.IGD != nil {
		return self.client.DeletePortMapping(``, uint16(mapping.ExternalPort), mapping.Protocol)
	} else {
		return fmt.Errorf("invalid mapping")
	}
}

func (self *IGD) GetLocalIP() (string, error) {
	host, _, _ := net.SplitHostPort(self.client.GetServiceClient().RootDevice.URLBase.Host)
	devIP := net.ParseIP(host)

	if devIP == nil {
		return ``, fmt.Errorf("Could not determine gateway local address")
	}

	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if a, ok := addr.(*net.IPNet); ok && a.Contains(devIP) {
						return a.IP.String(), nil
					}
				}
			} else {
				return ``, err
			}
		}

		return ``, fmt.Errorf("could not determine internal IP")
	} else {
		return ``, err
	}
}

func (self *IGD) GetExternalIP() (string, error) {
	return self.client.GetExternalIPAddress()
}
