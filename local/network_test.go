package local

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-network-runner/api"
	apimocks "github.com/ava-labs/avalanche-network-runner/api/mocks"
	"github.com/ava-labs/avalanche-network-runner/local/mocks"
	"github.com/ava-labs/avalanche-network-runner/network"
	"github.com/ava-labs/avalanche-network-runner/network/node"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

const (
	defaultHealthyTimeout = 5 * time.Second
)

var (
	_ NewNodeProcessF   = newMockProcessUndef
	_ NewNodeProcessF   = newMockProcessSuccessful
	_ NewNodeProcessF   = newMockProcessFailedStart
	_ api.NewAPIClientF = newMockAPISuccessful
	_ api.NewAPIClientF = newMockAPIUnhealthy
)

// Returns an API client where:
// * The Health API's Health method always returns healthy
// * The CChainEthAPI's Close method may be called
// * Only the above 2 methods may be called
// TODO have this method return an API Client that has all
// APIs and methods implemented
func newMockAPISuccessful(ipAddr string, port uint16, requestTimeout time.Duration) api.Client {
	healthReply := &health.APIHealthClientReply{Healthy: true}
	healthClient := &apimocks.HealthClient{}
	healthClient.On("Health").Return(healthReply, nil)
	// ethClient used when removing nodes, to close websocket connection
	ethClient := &apimocks.EthClient{}
	ethClient.On("Close").Return()
	client := &apimocks.Client{}
	client.On("HealthAPI").Return(healthClient)
	client.On("CChainEthAPI").Return(ethClient)
	return client
}

// Returns an API client where the Health API's Health method always returns unhealthy
func newMockAPIUnhealthy(ipAddr string, port uint16, requestTimeout time.Duration) api.Client {
	healthReply := &health.APIHealthClientReply{Healthy: false}
	healthClient := &apimocks.HealthClient{}
	healthClient.On("Health").Return(healthReply, nil)
	client := &apimocks.Client{}
	client.On("HealthAPI").Return(healthClient)
	return client
}

func newMockProcessUndef(node.Config, ...string) (NodeProcess, error) {
	return &mocks.NodeProcess{}, nil
}

// Returns a NodeProcess that always returns nil
func newMockProcessSuccessful(node.Config, ...string) (NodeProcess, error) {
	process := &mocks.NodeProcess{}
	process.On("Start").Return(nil)
	process.On("Wait").Return(nil)
	process.On("Stop").Return(nil)
	return process, nil
}

// Return a NodeProcess that returns an error when Start is called
func newMockProcessFailedStart(node.Config, ...string) (NodeProcess, error) {
	process := &mocks.NodeProcess{}
	process.On("Start").Return(errors.New("Start failed"))
	process.On("Wait").Return(nil)
	process.On("Stop").Return(nil)
	return process, nil
}

// Start a network with no nodes
func TestNewNetworkEmpty(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	networkConfig.NodeConfigs = nil
	net, err := newNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newMockProcessUndef,
	)
	assert.NoError(err)
	// Assert that GetNodesNames() returns an empty list
	names, err := net.GetNodesNames()
	assert.NoError(err)
	assert.Len(names, 0)
}

// Start a network with one node.
func TestNewNetworkOneNode(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	networkConfig.NodeConfigs = networkConfig.NodeConfigs[:1]
	// Assert that the node's config is being passed correctly
	// to the function that starts the node process.
	newProcessF := func(config node.Config, _ ...string) (NodeProcess, error) {
		assert.True(config.IsBeacon)
		assert.EqualValues(networkConfig.NodeConfigs[0], config)
		return newMockProcessSuccessful(config)
	}
	net, err := newNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newProcessF,
	)
	assert.NoError(err)

	// Assert that GetNodesNames() includes only the 1 node's name
	names, err := net.GetNodesNames()
	assert.NoError(err)
	assert.Contains(names, networkConfig.NodeConfigs[0].Name)
	assert.Len(names, 1)

	// Assert that the network's genesis was set
	assert.EqualValues(networkConfig.Genesis, net.(*localNetwork).genesis)
}

// Test that NewNetwork returns an error when
// starting a node returns an error
func TestNewNetworkFailToStartNode(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	_, err := newNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newMockProcessFailedStart,
	)
	assert.Error(err)
}

// Check configs that are expected to be invalid at network creation time
func TestWrongNetworkConfigs(t *testing.T) {
	refNetworkConfig := testNetworkConfig(t)
	tests := map[string]struct {
		config network.Config
	}{
		"no ImplSpecificConfig": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"config file unmarshal": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("nonempty"),
					},
				},
			},
		},
		"wrong network id type in config file": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"network-id\": \"0\"}"),
					},
				},
			},
		},
		"wrong db dir type in config": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"db-dir\": 0}"),
					},
				},
			},
		},
		"wrong log dir type in config": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"log-dir\": 0}"),
					},
				},
			},
		},
		"wrong http port type in config": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"http-port\": \"0\"}"),
					},
				},
			},
		},
		"wrong staking port type in config": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"staking-port\": \"0\"}"),
					},
				},
			},
		},
		"network id mismatch": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
						ConfigFile:  []byte("{\"network-id\": 1}"),
					},
				},
			},
		},
		"genesis unmarshall": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"no network id in genesis": {
			config: network.Config{
				Genesis: []byte("{}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"wrong network id type in genesis": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": \"0\"}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"no Genesis": {
			config: network.Config{
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"StakingKey but no StakingCert": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:   true,
						StakingKey: refNetworkConfig.NodeConfigs[0].StakingKey,
					},
				},
			},
		},
		"StakingCert but no StakingKey": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"invalid staking cert/key": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"no beacon node": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
				},
			},
		},
		"repeated name": {
			config: network.Config{
				Genesis: []byte("{\"networkID\": 0}"),
				NodeConfigs: []node.Config{
					{
						Name: "node0",
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[0].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[0].StakingCert,
					},
					{
						Name: "node0",
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  refNetworkConfig.NodeConfigs[1].StakingKey,
						StakingCert: refNetworkConfig.NodeConfigs[1].StakingCert,
					},
				},
			},
		},
	}
	assert := assert.New(t)
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := newNetwork(logging.NoLog{}, tt.config, newMockAPISuccessful, newMockProcessSuccessful)
			assert.Error(err)
		})
	}
}

// Give incorrect type to interface{} ImplSpecificConfig
func TestImplSpecificConfigInterface(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	networkConfig.NodeConfigs[0].ImplSpecificConfig = "should not be string"
	_, err := newNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.Error(err)
}

// Assert that the network's Healthy() method returns an
// error when all nodes' Health API return unhealthy
func TestUnhealthyNetwork(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	net, err := newNetwork(logging.NoLog{}, networkConfig, newMockAPIUnhealthy, newMockProcessSuccessful)
	assert.NoError(err)
	assert.Error(awaitNetworkHealthy(net, defaultHealthyTimeout))
}

// Create a network without giving names to nodes.
// Checks that the generated names are the correct number and unique.
func TestGeneratedNodesNames(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	for i := range networkConfig.NodeConfigs {
		networkConfig.NodeConfigs[i].Name = ""
	}
	net, err := newNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	nodeNameMap := make(map[string]bool)
	nodeNames, err := net.GetNodesNames()
	assert.NoError(err)
	for _, nodeName := range nodeNames {
		nodeNameMap[nodeName] = true
	}
	assert.EqualValues(len(nodeNameMap), len(networkConfig.NodeConfigs))
}

// TestGenerateDefaultNetwork create a default network with GenerateDefaultNetwork and
// check expected number of nodes, node names, and avalanchego node ids
func TestGenerateDefaultNetwork(t *testing.T) {
	assert := assert.New(t)
	binaryPath := "pepito"
	net, err := newDefaultNetwork(logging.NoLog{}, binaryPath, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	assert.NoError(awaitNetworkHealthy(net, defaultHealthyTimeout))
	names, err := net.GetNodesNames()
	assert.NoError(err)
	assert.Len(names, 5)
	for _, nodeInfo := range []struct {
		name string
		ID   string
	}{
		{
			"node-0",
			"NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
		},
		{
			"node-1",
			"NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
		},
		{
			"node-2",
			"NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN",
		},
		{
			"node-3",
			"NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu",
		},
		{
			"node-4",
			"NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
		},
	} {
		assert.Contains(names, nodeInfo.name)
		node, err := net.GetNode(nodeInfo.name)
		assert.NoError(err)
		assert.EqualValues(nodeInfo.name, node.GetName())
		expectedID, err := ids.ShortFromPrefixedString(nodeInfo.ID, constants.NodeIDPrefix)
		assert.NoError(err)
		assert.EqualValues(expectedID, node.GetNodeID())
	}
}

// TODO add byzantine node to conf
// TestNetworkFromConfig creates/waits/checks/stops a network from config file
// the check verify that all the nodes can be accessed
func TestNetworkFromConfig(t *testing.T) {
	assert := assert.New(t)
	networkConfig := testNetworkConfig(t)
	net, err := newNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	assert.NoError(awaitNetworkHealthy(net, defaultHealthyTimeout))
	runningNodes := make(map[string]struct{})
	for _, nodeConfig := range networkConfig.NodeConfigs {
		runningNodes[nodeConfig.Name] = struct{}{}
	}
	checkNetwork(t, net, runningNodes, nil)
}

// TestNetworkNodeOps creates an empty network,
// adds nodes one by one, then removes nodes one by one.
// Setween all operations, a network check is performed
// to verify that all the running nodes are in the network,
// and all removed nodes are not.
func TestNetworkNodeOps(t *testing.T) {
	assert := assert.New(t)

	// Start a new, empty network
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	net, err := newNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	runningNodes := make(map[string]struct{})

	// Add nodes to the network one by one
	networkConfig := testNetworkConfig(t)
	for _, nodeConfig := range networkConfig.NodeConfigs {
		_, err := net.AddNode(nodeConfig)
		assert.NoError(err)
		runningNodes[nodeConfig.Name] = struct{}{}
		checkNetwork(t, net, runningNodes, nil)
	}
	// Wait for all nodes to be healthy
	assert.NoError(awaitNetworkHealthy(net, defaultHealthyTimeout))

	// Remove nodes one by one
	removedNodes := make(map[string]struct{})
	for _, nodeConfig := range networkConfig.NodeConfigs {
		_, err := net.GetNode(nodeConfig.Name)
		assert.NoError(err)
		err = net.RemoveNode(nodeConfig.Name)
		assert.NoError(err)
		removedNodes[nodeConfig.Name] = struct{}{}
		delete(runningNodes, nodeConfig.Name)
		checkNetwork(t, net, runningNodes, removedNodes)
	}
}

// TestNodeNotFound checks all operations fail for an unknown node,
// being it either not created, or created and removed thereafter
func TestNodeNotFound(t *testing.T) {
	assert := assert.New(t)
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	networkConfig := testNetworkConfig(t)
	net, err := newNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	_, err = net.AddNode(networkConfig.NodeConfigs[0])
	assert.NoError(err)
	// get node
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.NoError(err)
	// get non-existent node
	_, err = net.GetNode(networkConfig.NodeConfigs[1].Name)
	assert.Error(err)
	// remove non-existent node
	err = net.RemoveNode(networkConfig.NodeConfigs[1].Name)
	assert.Error(err)
	// remove node
	err = net.RemoveNode(networkConfig.NodeConfigs[0].Name)
	assert.NoError(err)
	// get removed node
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.Error(err)
	// remove already-removed node
	err = net.RemoveNode(networkConfig.NodeConfigs[0].Name)
	assert.Error(err)
}

// TestStoppedNetwork checks that operations fail for an already stopped network
func TestStoppedNetwork(t *testing.T) {
	assert := assert.New(t)
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	networkConfig := testNetworkConfig(t)
	net, err := newNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	_, err = net.AddNode(networkConfig.NodeConfigs[0])
	assert.NoError(err)
	// first GetNodesNames should return some nodes
	_, err = net.GetNodesNames()
	assert.NoError(err)
	err = net.Stop(context.TODO())
	assert.NoError(err)
	// Stop failure
	assert.EqualValues(net.Stop(context.TODO()), network.ErrStopped)
	// AddNode failure
	_, err = net.AddNode(networkConfig.NodeConfigs[1])
	assert.EqualValues(err, network.ErrStopped)
	// GetNode failure
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.EqualValues(err, network.ErrStopped)
	// second GetNodesNames should return no nodes
	_, err = net.GetNodesNames()
	assert.EqualValues(err, network.ErrStopped)
	// RemoveNode failure
	assert.EqualValues(net.RemoveNode(networkConfig.NodeConfigs[0].Name), network.ErrStopped)
	// Healthy failure
	assert.EqualValues(awaitNetworkHealthy(net, defaultHealthyTimeout), network.ErrStopped)
}

// checkNetwork receives a network, a set of running nodes (started and not removed yet), and
// a set of removed nodes, checking:
// - GetNodeNames retrieves the correct number of running nodes
// - GetNode does not fail for given running nodes
// - GetNode does fail for given stopped nodes
func checkNetwork(t *testing.T, net network.Network, runningNodes map[string]struct{}, removedNodes map[string]struct{}) {
	assert := assert.New(t)
	nodeNames, err := net.GetNodesNames()
	assert.NoError(err)
	assert.EqualValues(len(nodeNames), len(runningNodes))
	for nodeName := range runningNodes {
		_, err := net.GetNode(nodeName)
		assert.NoError(err)
	}
	for nodeName := range removedNodes {
		_, err := net.GetNode(nodeName)
		assert.Error(err)
	}
}

// Return a network config that has no nodes
func emptyNetworkConfig() (network.Config, error) {
	networkID := uint32(1337)
	// Use a dummy genesis
	genesis, err := network.NewAvalancheGoGenesis(
		logging.NoLog{},
		networkID,
		[]network.AddrAndBalance{
			{
				Addr:    ids.GenerateTestShortID(),
				Balance: 1,
			},
		},
		nil,
		[]ids.ShortID{ids.GenerateTestShortID()},
	)
	if err != nil {
		return network.Config{}, err
	}
	return network.Config{
		LogLevel: "DEBUG",
		Name:     "My Network",
		Genesis:  genesis,
	}, nil
}

// Returns a config for a three node network,
// where the nodes have randomly generated staking
// kets and certificates.
func testNetworkConfig(t *testing.T) network.Config {
	assert := assert.New(t)
	networkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	for i := 0; i < 3; i++ {
		nodeConfig := node.Config{
			Name: fmt.Sprintf("node%d", i),
			ImplSpecificConfig: NodeConfig{
				BinaryPath: "pepito",
			},
		}
		nodeConfig.StakingCert, nodeConfig.StakingKey, err = staking.NewCertAndKeyBytes()
		assert.NoError(err)
		networkConfig.NodeConfigs = append(networkConfig.NodeConfigs, nodeConfig)
	}
	networkConfig.NodeConfigs[0].IsBeacon = true
	return networkConfig
}

// Returns nil when all the nodes in [net] are healthy,
// or an error if one doesn't become healthy within
// the timeout.
func awaitNetworkHealthy(net network.Network, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	healthyCh := net.Healthy(ctx)
	return <-healthyCh
}
