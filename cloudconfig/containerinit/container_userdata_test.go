// Copyright 2015 Canonical Ltd.
// Copyright 2015 Cloudbase Solutions SRL
// Licensed under the AGPLv3, see LICENCE file for details.

package containerinit_test

import (
	"fmt"
	"path/filepath"
	"strings"
	stdtesting "testing"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/juju/juju/cloudconfig/cloudinit"
	"github.com/juju/juju/cloudconfig/containerinit"
	"github.com/juju/juju/container"
	containertesting "github.com/juju/juju/container/testing"
	"github.com/juju/juju/feature"
	"github.com/juju/juju/network"
	"github.com/juju/juju/service"
	systemdtesting "github.com/juju/juju/service/systemd/testing"
	"github.com/juju/juju/testing"
)

func Test(t *stdtesting.T) {
	gc.TestingT(t)
}

type UserDataSuite struct {
	testing.BaseSuite

	networkInterfacesFile string
	fakeInterfaces        []network.InterfaceInfo
	expectedNetConfig     string
}

var _ = gc.Suite(&UserDataSuite{})

func (s *UserDataSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.networkInterfacesFile = filepath.Join(c.MkDir(), "interfaces")
	s.fakeInterfaces = []network.InterfaceInfo{{
		InterfaceName:    "eth0",
		CIDR:             "0.1.2.0/24",
		ConfigType:       network.ConfigStatic,
		NoAutoStart:      false,
		Address:          network.NewAddress("0.1.2.3"),
		DNSServers:       network.NewAddresses("ns1.invalid", "ns2.invalid"),
		DNSSearchDomains: []string{"foo", "bar"},
		GatewayAddress:   network.NewAddress("0.1.2.1"),
		MACAddress:       "aa:bb:cc:dd:ee:f0",
	}, {
		InterfaceName:    "eth1",
		CIDR:             "0.1.2.0/24",
		ConfigType:       network.ConfigStatic,
		NoAutoStart:      false,
		Address:          network.NewAddress("0.1.2.4"),
		DNSServers:       network.NewAddresses("ns1.invalid", "ns2.invalid"),
		DNSSearchDomains: []string{"foo", "bar"},
		GatewayAddress:   network.NewAddress("0.1.2.1"),
		MACAddress:       "aa:bb:cc:dd:ee:f0",
	}, {
		InterfaceName: "eth2",
		ConfigType:    network.ConfigDHCP,
		NoAutoStart:   true,
	}, {
		InterfaceName: "eth3",
		ConfigType:    network.ConfigDHCP,
		NoAutoStart:   false,
	}, {
		InterfaceName: "eth4",
		ConfigType:    network.ConfigManual,
		NoAutoStart:   true,
	}}
	s.expectedNetConfig = `
auto eth0 eth1 eth3 lo

iface lo inet loopback
  dns-nameservers ns1.invalid ns2.invalid
  dns-search bar foo

iface eth0 inet static
  address 0.1.2.3/24
  gateway 0.1.2.1

iface eth1 inet static
  address 0.1.2.4/24

iface eth2 inet dhcp

iface eth3 inet dhcp

iface eth4 inet manual
`
	s.PatchValue(containerinit.NetworkInterfacesFile, s.networkInterfacesFile)
}

func (s *UserDataSuite) TestGenerateNetworkConfig(c *gc.C) {
	data, err := containerinit.GenerateNetworkConfig(nil)
	c.Assert(err, gc.ErrorMatches, "missing container network config")
	c.Assert(data, gc.Equals, "")
	netConfig := container.BridgeNetworkConfig("foo", 0, nil)
	data, err = containerinit.GenerateNetworkConfig(netConfig)
	c.Assert(err, gc.ErrorMatches, "missing container network config")
	c.Assert(data, gc.Equals, "")

	// Test with all interface types.
	netConfig = container.BridgeNetworkConfig("foo", 0, s.fakeInterfaces)
	data, err = containerinit.GenerateNetworkConfig(netConfig)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(data, gc.Equals, s.expectedNetConfig)
}

func (s *UserDataSuite) TestNewCloudInitConfigWithNetworks(c *gc.C) {
	netConfig := container.BridgeNetworkConfig("foo", 0, s.fakeInterfaces)
	cloudConf, err := containerinit.NewCloudInitConfigWithNetworks("quantal", netConfig)
	c.Assert(err, jc.ErrorIsNil)
	// We need to indent expectNetConfig to make it valid YAML,
	// dropping the last new line and using unindented blank lines.
	lines := strings.Split(s.expectedNetConfig, "\n")
	indentedNetConfig := strings.Join(lines[:len(lines)-2], "\n  ")
	indentedNetConfig = strings.Replace(indentedNetConfig, "\n  \n", "\n\n", -1)
	expected := `
#cloud-config
bootcmd:
- install -D -m 644 /dev/null '%[1]s'
- |-
  printf '%%s\n' '
  auto eth0 eth1 eth3 lo

  iface lo inet loopback
    dns-nameservers ns1.invalid ns2.invalid
    dns-search bar foo

  iface eth0 inet static
    address 0.1.2.3/24
    gateway 0.1.2.1

  iface eth1 inet static
    address 0.1.2.4/24

  iface eth2 inet dhcp

  iface eth3 inet dhcp

  iface eth4 inet manual
  ' > '%[1]s'
runcmd:
- ifup -a || true
`[1:]
	assertUserData(c, cloudConf, fmt.Sprintf(expected, s.networkInterfacesFile))
}

func (s *UserDataSuite) TestNewCloudInitConfigWithNetworksNoConfig(c *gc.C) {
	netConfig := container.BridgeNetworkConfig("foo", 0, nil)
	cloudConf, err := containerinit.NewCloudInitConfigWithNetworks("quantal", netConfig)
	c.Assert(err, gc.ErrorMatches, "missing container network config")
	c.Assert(cloudConf, gc.IsNil)
}

func (s *UserDataSuite) TestCloudInitUserData(c *gc.C) {
	instanceConfig, err := containertesting.MockMachineConfig("1/lxc/0")
	c.Assert(err, jc.ErrorIsNil)
	networkConfig := container.BridgeNetworkConfig("foo", 0, nil)
	data, err := containerinit.CloudInitUserData(instanceConfig, networkConfig)
	c.Assert(err, gc.ErrorMatches, "missing container network config")
	c.Assert(data, gc.IsNil)
}

func assertUserData(c *gc.C, cloudConf cloudinit.CloudConfig, expected string) {
	data, err := cloudConf.RenderYAML()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(data), gc.Equals, expected)
	// Make sure it's valid YAML as well.
	out := make(map[string]interface{})
	err = yaml.Unmarshal(data, &out)
	c.Assert(err, jc.ErrorIsNil)
	if len(cloudConf.BootCmds()) > 0 {
		outcmds := out["bootcmd"].([]interface{})
		confcmds := cloudConf.BootCmds()
		c.Assert(len(outcmds), gc.Equals, len(confcmds))
		for i, _ := range outcmds {
			c.Assert(outcmds[i].(string), gc.Equals, confcmds[i])
		}
	} else {
		c.Assert(out["bootcmd"], gc.IsNil)
	}
}

func (s *UserDataSuite) TestShutdownInitCommandsUpstart(c *gc.C) {
	s.SetFeatureFlags(feature.AddressAllocation)
	cmds, err := containerinit.ShutdownInitCommands(service.InitSystemUpstart, "trusty")
	c.Assert(err, jc.ErrorIsNil)

	filename := "/etc/init/juju-template-restart.conf"
	script := `
description "juju shutdown job"
author "Juju Team <juju@lists.ubuntu.com>"
start on stopped cloud-final

script
  /bin/cat > /etc/network/interfaces << EOC
# loopback interface
auto lo
iface lo inet loopback

# primary interface
auto eth0
iface eth0 inet dhcp
EOC
  /bin/rm -fr /var/lib/dhcp/dhclient* /var/log/cloud-init*.log
  /sbin/shutdown -h now
end script

post-stop script
  rm /etc/init/juju-template-restart.conf
end script
`[1:]
	c.Check(cmds, gc.HasLen, 1)
	testing.CheckWriteFileCommand(c, cmds[0], filename, script, nil)
}

func (s *UserDataSuite) TestShutdownInitCommandsSystemd(c *gc.C) {
	s.SetFeatureFlags(feature.AddressAllocation)
	commands, err := containerinit.ShutdownInitCommands(service.InitSystemSystemd, "vivid")
	c.Assert(err, jc.ErrorIsNil)

	test := systemdtesting.WriteConfTest{
		Service: "juju-template-restart",
		DataDir: "/var/lib/juju",
		Expected: `
[Unit]
Description=juju shutdown job
After=syslog.target
After=network.target
After=systemd-user-sessions.service
After=cloud-config.target

[Service]
ExecStart=/var/lib/juju/init/juju-template-restart/exec-start.sh
ExecStopPost=/bin/systemctl disable juju-template-restart.service

[Install]
WantedBy=multi-user.target
`[1:],
		Script: `
/bin/cat > /etc/network/interfaces << EOC
# loopback interface
auto lo
iface lo inet loopback

# primary interface
auto eth0
iface eth0 inet dhcp
EOC
  /bin/rm -fr /var/lib/dhcp/dhclient* /var/log/cloud-init*.log
  /sbin/shutdown -h now`[1:],
	}
	test.CheckInstallAndStartCommands(c, commands)
}
