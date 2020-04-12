package util

import (
	"errors"
	"net"
)

func GetMyIP() (string, error) {
	iFaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	_, ipNetPrivate1,_ := net.ParseCIDR("192.168.0.0/16") // Usual internal network
	_, ipNetPrivate2,_ := net.ParseCIDR("172.0.0.0/8") // Docker interfaces
	// Google internals 10.x.y.z shall be considered valid
	isInLocalNetwork := func(iIP net.IP)bool{
		return ipNetPrivate1.Contains(iIP) || ipNetPrivate2.Contains(iIP)
	}

	for _, iFace := range iFaces {
		if iFace.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iFace.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		ipAddresses, err := iFace.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range ipAddresses {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || isInLocalNetwork(ip) {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("cannon get public IP address")
}
