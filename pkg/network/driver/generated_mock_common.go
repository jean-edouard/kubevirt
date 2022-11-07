// Automatically generated by MockGen. DO NOT EDIT!
// Source: common.go

package driver

import (
	net "net"

	iptables "github.com/coreos/go-iptables/iptables"
	gomock "github.com/golang/mock/gomock"
	netlink "github.com/vishvananda/netlink"
	v1 "kubevirt.io/api/core/v1"

	cache "kubevirt.io/kubevirt/pkg/network/cache"
)

// Mock of NetworkHandler interface
type MockNetworkHandler struct {
	ctrl     *gomock.Controller
	recorder *_MockNetworkHandlerRecorder
}

// Recorder for MockNetworkHandler (not exported)
type _MockNetworkHandlerRecorder struct {
	mock *MockNetworkHandler
}

func NewMockNetworkHandler(ctrl *gomock.Controller) *MockNetworkHandler {
	mock := &MockNetworkHandler{ctrl: ctrl}
	mock.recorder = &_MockNetworkHandlerRecorder{mock}
	return mock
}

func (_m *MockNetworkHandler) EXPECT() *_MockNetworkHandlerRecorder {
	return _m.recorder
}

func (_m *MockNetworkHandler) LinkByName(name string) (netlink.Link, error) {
	ret := _m.ctrl.Call(_m, "LinkByName", name)
	ret0, _ := ret[0].(netlink.Link)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) LinkByName(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkByName", arg0)
}

func (_m *MockNetworkHandler) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	ret := _m.ctrl.Call(_m, "AddrList", link, family)
	ret0, _ := ret[0].([]netlink.Addr)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) AddrList(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "AddrList", arg0, arg1)
}

func (_m *MockNetworkHandler) ReadIPAddressesFromLink(interfaceName string) (string, string, error) {
	ret := _m.ctrl.Call(_m, "ReadIPAddressesFromLink", interfaceName)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(string)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

func (_mr *_MockNetworkHandlerRecorder) ReadIPAddressesFromLink(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ReadIPAddressesFromLink", arg0)
}

func (_m *MockNetworkHandler) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	ret := _m.ctrl.Call(_m, "RouteList", link, family)
	ret0, _ := ret[0].([]netlink.Route)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) RouteList(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "RouteList", arg0, arg1)
}

func (_m *MockNetworkHandler) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	ret := _m.ctrl.Call(_m, "AddrDel", link, addr)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) AddrDel(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "AddrDel", arg0, arg1)
}

func (_m *MockNetworkHandler) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	ret := _m.ctrl.Call(_m, "AddrAdd", link, addr)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) AddrAdd(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "AddrAdd", arg0, arg1)
}

func (_m *MockNetworkHandler) AddrReplace(link netlink.Link, addr *netlink.Addr) error {
	ret := _m.ctrl.Call(_m, "AddrReplace", link, addr)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) AddrReplace(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "AddrReplace", arg0, arg1)
}

func (_m *MockNetworkHandler) LinkSetDown(link netlink.Link) error {
	ret := _m.ctrl.Call(_m, "LinkSetDown", link)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetDown(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetDown", arg0)
}

func (_m *MockNetworkHandler) LinkSetUp(link netlink.Link) error {
	ret := _m.ctrl.Call(_m, "LinkSetUp", link)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetUp(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetUp", arg0)
}

func (_m *MockNetworkHandler) LinkSetName(link netlink.Link, name string) error {
	ret := _m.ctrl.Call(_m, "LinkSetName", link, name)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetName(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetName", arg0, arg1)
}

func (_m *MockNetworkHandler) LinkAdd(link netlink.Link) error {
	ret := _m.ctrl.Call(_m, "LinkAdd", link)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkAdd(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkAdd", arg0)
}

func (_m *MockNetworkHandler) LinkSetLearningOff(link netlink.Link) error {
	ret := _m.ctrl.Call(_m, "LinkSetLearningOff", link)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetLearningOff(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetLearningOff", arg0)
}

func (_m *MockNetworkHandler) ParseAddr(s string) (*netlink.Addr, error) {
	ret := _m.ctrl.Call(_m, "ParseAddr", s)
	ret0, _ := ret[0].(*netlink.Addr)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) ParseAddr(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ParseAddr", arg0)
}

func (_m *MockNetworkHandler) LinkSetHardwareAddr(link netlink.Link, hwaddr net.HardwareAddr) error {
	ret := _m.ctrl.Call(_m, "LinkSetHardwareAddr", link, hwaddr)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetHardwareAddr(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetHardwareAddr", arg0, arg1)
}

func (_m *MockNetworkHandler) LinkSetMaster(link netlink.Link, master *netlink.Bridge) error {
	ret := _m.ctrl.Call(_m, "LinkSetMaster", link, master)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) LinkSetMaster(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "LinkSetMaster", arg0, arg1)
}

func (_m *MockNetworkHandler) StartDHCP(nic *cache.DHCPConfig, bridgeInterfaceName string, dhcpOptions *v1.DHCPOptions) error {
	ret := _m.ctrl.Call(_m, "StartDHCP", nic, bridgeInterfaceName, dhcpOptions)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) StartDHCP(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "StartDHCP", arg0, arg1, arg2)
}

func (_m *MockNetworkHandler) HasIPv4GlobalUnicastAddress(interfaceName string) (bool, error) {
	ret := _m.ctrl.Call(_m, "HasIPv4GlobalUnicastAddress", interfaceName)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) HasIPv4GlobalUnicastAddress(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "HasIPv4GlobalUnicastAddress", arg0)
}

func (_m *MockNetworkHandler) HasIPv6GlobalUnicastAddress(interfaceName string) (bool, error) {
	ret := _m.ctrl.Call(_m, "HasIPv6GlobalUnicastAddress", interfaceName)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) HasIPv6GlobalUnicastAddress(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "HasIPv6GlobalUnicastAddress", arg0)
}

func (_m *MockNetworkHandler) IsIpv4Primary() (bool, error) {
	ret := _m.ctrl.Call(_m, "IsIpv4Primary")
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockNetworkHandlerRecorder) IsIpv4Primary() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "IsIpv4Primary")
}

func (_m *MockNetworkHandler) ConfigureIpForwarding(proto iptables.Protocol) error {
	ret := _m.ctrl.Call(_m, "ConfigureIpForwarding", proto)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) ConfigureIpForwarding(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ConfigureIpForwarding", arg0)
}

func (_m *MockNetworkHandler) ConfigureRouteLocalNet(_param0 string) error {
	ret := _m.ctrl.Call(_m, "ConfigureRouteLocalNet", _param0)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) ConfigureRouteLocalNet(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ConfigureRouteLocalNet", arg0)
}

func (_m *MockNetworkHandler) ConfigureIpv4ArpIgnore() error {
	ret := _m.ctrl.Call(_m, "ConfigureIpv4ArpIgnore")
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) ConfigureIpv4ArpIgnore() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ConfigureIpv4ArpIgnore")
}

func (_m *MockNetworkHandler) ConfigurePingGroupRange() error {
	ret := _m.ctrl.Call(_m, "ConfigurePingGroupRange")
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) ConfigurePingGroupRange() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ConfigurePingGroupRange")
}

func (_m *MockNetworkHandler) ConfigureUnprivilegedPortStart(_param0 string) error {
	ret := _m.ctrl.Call(_m, "ConfigureUnprivilegedPortStart", _param0)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) ConfigureUnprivilegedPortStart(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "ConfigureUnprivilegedPortStart", arg0)
}

func (_m *MockNetworkHandler) NftablesNewChain(proto iptables.Protocol, table string, chain string) error {
	ret := _m.ctrl.Call(_m, "NftablesNewChain", proto, table, chain)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) NftablesNewChain(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "NftablesNewChain", arg0, arg1, arg2)
}

func (_m *MockNetworkHandler) NftablesNewTable(proto iptables.Protocol, name string) error {
	ret := _m.ctrl.Call(_m, "NftablesNewTable", proto, name)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) NftablesNewTable(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "NftablesNewTable", arg0, arg1)
}

func (_m *MockNetworkHandler) NftablesAppendRule(proto iptables.Protocol, table string, chain string, rulespec ...string) error {
	_s := []interface{}{proto, table, chain}
	for _, _x := range rulespec {
		_s = append(_s, _x)
	}
	ret := _m.ctrl.Call(_m, "NftablesAppendRule", _s...)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) NftablesAppendRule(arg0, arg1, arg2 interface{}, arg3 ...interface{}) *gomock.Call {
	_s := append([]interface{}{arg0, arg1, arg2}, arg3...)
	return _mr.mock.ctrl.RecordCall(_mr.mock, "NftablesAppendRule", _s...)
}

func (_m *MockNetworkHandler) CheckNftables() error {
	ret := _m.ctrl.Call(_m, "CheckNftables")
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) CheckNftables() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "CheckNftables")
}

func (_m *MockNetworkHandler) GetNFTIPString(proto iptables.Protocol) string {
	ret := _m.ctrl.Call(_m, "GetNFTIPString", proto)
	ret0, _ := ret[0].(string)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) GetNFTIPString(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "GetNFTIPString", arg0)
}

func (_m *MockNetworkHandler) CreateTapDevice(tapName string, queueNumber uint32, launcherPID int, mtu int, tapOwner string) error {
	ret := _m.ctrl.Call(_m, "CreateTapDevice", tapName, queueNumber, launcherPID, mtu, tapOwner)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) CreateTapDevice(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "CreateTapDevice", arg0, arg1, arg2, arg3, arg4)
}

func (_m *MockNetworkHandler) BindTapDeviceToBridge(tapName string, bridgeName string) error {
	ret := _m.ctrl.Call(_m, "BindTapDeviceToBridge", tapName, bridgeName)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) BindTapDeviceToBridge(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "BindTapDeviceToBridge", arg0, arg1)
}

func (_m *MockNetworkHandler) DisableTXOffloadChecksum(ifaceName string) error {
	ret := _m.ctrl.Call(_m, "DisableTXOffloadChecksum", ifaceName)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockNetworkHandlerRecorder) DisableTXOffloadChecksum(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "DisableTXOffloadChecksum", arg0)
}
