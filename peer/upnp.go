package peer

import (
	"fmt"
	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
	"net"
	"strings"
	"time"
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
	client          igdClient
	ExternalAddress string
}

func DiscoverGateways(timeout time.Duration) ([]*IGD, error) {
	discochan := make(chan igdDiscoveryResponse)
	devices := make([]*IGD, 0)

	go func() {
		igdDevices := make([]igdClient, 0)
		var lastError error

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
				if externalAddr, err := igdDevice.GetExternalIPAddress(); err == nil {
					devices = append(devices, &IGD{
						ExternalAddress: externalAddr,
						client:          igdDevice,
					})
				} else {
					log.Warningf("Failed to get external address for device %s: %v", igdDevice.GetServiceClient().Location, err)
				}
			}
		} else {
			return nil, discovery.Error
		}
	case <-time.After(timeout):
		return nil, fmt.Errorf("UPnP device discovery timed out after %s", timeout)
	}

	return devices, nil
}

func (self *IGD) AddPortMapping(protocol string, internalPort int, externalPort int, description string, leaseDuration time.Duration) error {
	protocol = strings.ToUpper(protocol)

	if self.ExternalAddress == `` {
		return fmt.Errorf("Cannot add port mapping to IGD: external address not found")
	}

	if localAddr, err := self.getInternalIP(); err == nil {
		log.Debugf("Adding port mapping: %s:%d/%s -> %s:%d/%s",
			self.ExternalAddress, externalPort, protocol,
			localAddr, internalPort, protocol)

		return self.client.AddPortMapping(
			self.ExternalAddress,
			uint16(externalPort),
			protocol,
			uint16(internalPort),
			localAddr,
			true,
			description,
			uint32(leaseDuration/time.Second))
	} else {
		return err
	}
}

func (self *IGD) DeletePortMapping(protocol string, port int) error {
	protocol = strings.ToUpper(protocol)

	if self.ExternalAddress == `` {
		return fmt.Errorf("Cannot add port mapping to IGD: external address not found")
	}

	return self.client.DeletePortMapping(self.ExternalAddress, uint16(port), protocol)
}

func (self *IGD) getInternalIP() (string, error) {
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
