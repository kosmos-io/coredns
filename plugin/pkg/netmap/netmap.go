package netmap

import (
	"encoding/binary"
	"fmt"
	"net"
)

type IPType int

const (
	IPV4 IPType = iota
	IPV6
)

func GetIPType(s string) IPType {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			return IPV4
		case ':':
			return IPV6
		}
	}
	return -1
}

func NetMap(ipStr string, cidrsMap map[string]string) (string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr, nil
	}
	for src, dest := range cidrsMap {
		_, srcNet, err := net.ParseCIDR(src)
		if err != nil {
			return "", err
		}

		if GetIPType(ipStr) != GetIPType(src) {
			continue
		}

		if !srcNet.Contains(ip) {
			continue
		}

		_, destNet, err := net.ParseCIDR(dest)
		if err != nil {
			return "", err
		}

		srcBits, _ := srcNet.Mask.Size()
		destBits, _ := destNet.Mask.Size()
		if srcBits != destBits {
			return "", fmt.Errorf("the subnet masks of srcCIDR and destCIDR of CIDRsMap need to be the same")
		}

		var changeIPNet func(ip net.IP, destNet net.IPNet) (net.IP, error)

		if GetIPType(ipStr) == IPV4 {
			changeIPNet = changeIPNetIPV4
		} else {
			changeIPNet = changeIPNetIPV6
		}

		newIP, err := changeIPNet(ip, *destNet)
		if err != nil {
			return "", err
		}
		return newIP.String(), nil
	}

	return ip.String(), nil
}

func changeIPNetIPV4(ip net.IP, destNet net.IPNet) (net.IP, error) {
	ipBytes := ip.To4()
	destNetBytes := destNet.IP.To4()
	maskSize, _ := destNet.Mask.Size()

	ipBits := binary.BigEndian.Uint32(ipBytes)
	destNetBits := binary.BigEndian.Uint32(destNetBytes)

	v := ((destNetBits >> (32 - maskSize)) << (32 - maskSize)) | ((ipBits << maskSize) >> maskSize)

	newIP := make(net.IP, 4)
	binary.BigEndian.PutUint32(newIP, v)

	return newIP, nil
}

func changeIPNetIPV6(ip net.IP, destNet net.IPNet) (net.IP, error) {
	ipBytes := []byte(ip)
	maskBytes := []byte(destNet.Mask)
	destIPBytes := []byte(destNet.IP)

	targetIP := make(net.IP, len(ipBytes))

	for k, _ := range ipBytes {
		invertedMask := maskBytes[k] ^ 0xff
		targetIP[k] = (invertedMask & ipBytes[k]) | (destIPBytes[k] & maskBytes[k])
	}

	return targetIP, nil
}
