// Cytoscape package provides conversion from our graph to the CystoscapeJS
// configuration json model.
//
// The following links are useful for understanding CytoscapeJS and it's configuration:
//
// Main page:   http://js.cytoscape.org/
// JSON config: http://js.cytoscape.org/#notation/elements-json
// Demos:       http://js.cytoscape.org/#demos
//
// Algorithm: Process the graph structure adding nodes and edges, decorating each
//            with information provided.  An optional second pass generates compound
//            nodes for version grouping.
//
package cytoscape

import (
	"crypto/md5"
	"fmt"
	"sort"

	"github.com/kiali/kiali/graph"
	"github.com/kiali/kiali/graph/options"
)

type NodeData struct {
	// Cytoscape Fields
	Id     string `json:"id"`               // unique internal node ID (n0, n1...)
	Parent string `json:"parent,omitempty"` // Compound Node parent ID

	// App Fields (not required by Cytoscape)
	NodeType        string          `json:"nodeType"`
	Namespace       string          `json:"namespace"`
	Workload        string          `json:"workload,omitempty"`
	App             string          `json:"app,omitempty"`
	Version         string          `json:"version,omitempty"`
	Service         string          `json:"service,omitempty"`         // requested service for NodeTypeService
	DestServices    map[string]bool `json:"destServices,omitempty"`    // requested services for [dest] node
	Rate            string          `json:"rate,omitempty"`            // edge aggregate
	Rate3xx         string          `json:"rate3XX,omitempty"`         // edge aggregate
	Rate4xx         string          `json:"rate4XX,omitempty"`         // edge aggregate
	Rate5xx         string          `json:"rate5XX,omitempty"`         // edge aggregate
	RateOut         string          `json:"rateOut,omitempty"`         // edge aggregate
	RateTcpSent     string          `json:"rateTcpSent,omitempty"`     // edge aggregate
	RateTcpSentOut  string          `json:"rateTcpSentOut,omitempty"`  // edge aggregate
	HasCB           bool            `json:"hasCB,omitempty"`           // true (has circuit breaker) | false
	HasMissingSC    bool            `json:"hasMissingSC,omitempty"`    // true (has missing sidecar) | false
	HasVS           bool            `json:"hasVS,omitempty"`           // true (has route rule) | false
	IsDead          bool            `json:"isDead,omitempty"`          // true (has no pods) | false
	IsEgress        bool            `json:"isEgress,omitempty"`        // true | false
	IsGroup         string          `json:"isGroup,omitempty"`         // set to the grouping type, current values: [ 'version' ]
	IsInaccessible  bool            `json:"isInaccessible,omitempty"`  // true if the node exists in an inaccessible namespace
	IsMisconfigured string          `json:"isMisconfigured,omitempty"` // set to misconfiguration list, current values: [ 'labels' ]
	IsOutside       bool            `json:"isOutside,omitempty"`       // true | false
	IsRoot          bool            `json:"isRoot,omitempty"`          // true | false
	IsUnused        bool            `json:"isUnused,omitempty"`        // true | false
}

type EdgeData struct {
	// Cytoscape Fields
	Id     string `json:"id"`     // unique internal edge ID (e0, e1...)
	Source string `json:"source"` // parent node ID
	Target string `json:"target"` // child node ID

	// App Fields (not required by Cytoscape)
	Rate         string `json:"rate,omitempty"`
	Rate3xx      string `json:"rate3XX,omitempty"`
	Rate4xx      string `json:"rate4XX,omitempty"`
	Rate5xx      string `json:"rate5XX,omitempty"`
	PercentErr   string `json:"percentErr,omitempty"`
	PercentRate  string `json:"percentRate,omitempty"` // percent of total parent requests
	ResponseTime string `json:"responseTime,omitempty"`
	IsMTLS       bool   `json:"isMTLS,omitempty"`   // true (mutual TLS connection) | false
	IsUnused     bool   `json:"isUnused,omitempty"` // true | false
	TcpSentRate  string `json:"tcpSentRate,omitempty"`
}

type NodeWrapper struct {
	Data *NodeData `json:"data"`
}

type EdgeWrapper struct {
	Data *EdgeData `json:"data"`
}

type Elements struct {
	Nodes []*NodeWrapper `json:"nodes"`
	Edges []*EdgeWrapper `json:"edges"`
}

type Config struct {
	Timestamp int64    `json:"timestamp"`
	GraphType string   `json:"graphType"`
	Elements  Elements `json:"elements"`
}

func nodeHash(id string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(id)))
}

func edgeHash(from, to, protocol string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s.%s.%s", from, to, protocol))))
}

func NewConfig(trafficMap graph.TrafficMap, o options.VendorOptions) (result Config) {
	nodes := []*NodeWrapper{}
	edges := []*EdgeWrapper{}

	buildConfig(trafficMap, &nodes, &edges, o)

	// Add compound nodes that group together different versions of the same node
	if o.GraphType == graph.GraphTypeVersionedApp && o.GroupBy == options.GroupByVersion {
		groupByVersion(&nodes)
	}

	// sort nodes and edges for better json presentation (and predictable testing)
	// kiali-1258 compound/isGroup/parent nodes must come before the child references
	sort.Slice(nodes, func(i, j int) bool {
		switch {
		case nodes[i].Data.Namespace != nodes[j].Data.Namespace:
			return nodes[i].Data.Namespace < nodes[j].Data.Namespace
		case nodes[i].Data.IsGroup != nodes[j].Data.IsGroup:
			return nodes[i].Data.IsGroup > nodes[j].Data.IsGroup
		case nodes[i].Data.App != nodes[j].Data.App:
			return nodes[i].Data.App < nodes[j].Data.App
		case nodes[i].Data.Version != nodes[j].Data.Version:
			return nodes[i].Data.Version < nodes[j].Data.Version
		case nodes[i].Data.Service != nodes[j].Data.Service:
			return nodes[i].Data.Service < nodes[j].Data.Service
		default:
			return nodes[i].Data.Workload < nodes[j].Data.Workload
		}
	})
	sort.Slice(edges, func(i, j int) bool {
		switch {
		case edges[i].Data.Source < edges[j].Data.Source:
			return true
		case edges[i].Data.Source > edges[j].Data.Source:
			return false
		default:
			return edges[i].Data.Target < edges[j].Data.Target
		}
	})

	elements := Elements{nodes, edges}
	result = Config{
		Timestamp: o.Timestamp,
		GraphType: o.GraphType,
		Elements:  elements,
	}
	return result
}

func buildConfig(trafficMap graph.TrafficMap, nodes *[]*NodeWrapper, edges *[]*EdgeWrapper, o options.VendorOptions) {
	for id, n := range trafficMap {
		nodeId := nodeHash(id)

		nd := &NodeData{
			Id:        nodeId,
			NodeType:  n.NodeType,
			Namespace: n.Namespace,
			Workload:  n.Workload,
			App:       n.App,
			Version:   n.Version,
			Service:   n.Service,
		}

		addNodeTelemetry(n, nd)

		// node may have deployment but no pods running)
		if val, ok := n.Metadata["isDead"]; ok {
			nd.IsDead = val.(bool)
		}

		// node may be a root
		if val, ok := n.Metadata["isRoot"]; ok {
			nd.IsRoot = val.(bool)
		}

		// node may be unused
		if val, ok := n.Metadata["isUnused"]; ok {
			nd.IsUnused = val.(bool)
		}

		// node is not accessible to the current user
		if val, ok := n.Metadata["isInaccessible"]; ok {
			nd.IsInaccessible = val.(bool)
		}

		// node may have a circuit breaker
		if val, ok := n.Metadata["hasCB"]; ok {
			nd.HasCB = val.(bool)
		}

		// node may have a virtual service
		if val, ok := n.Metadata["hasVS"]; ok {
			nd.HasVS = val.(bool)
		}

		// set sidecars checks, if available
		if val, ok := n.Metadata["hasMissingSC"]; ok {
			nd.HasMissingSC = val.(bool)
		}

		// check if node is misconfigured
		if val, ok := n.Metadata["isMisconfigured"]; ok {
			nd.IsMisconfigured = val.(string)
		}

		// check if node is on another namespace
		if val, ok := n.Metadata["isOutside"]; ok {
			nd.IsOutside = val.(bool)
		}

		// node may have destination service info
		if val, ok := n.Metadata["destServices"]; ok {
			nd.DestServices = val.(map[string]bool)
		}

		// node may be an egress service
		if val, ok := n.Metadata["isEgress"]; ok {
			nd.IsEgress = val.(bool)
		}

		nw := NodeWrapper{
			Data: nd,
		}

		*nodes = append(*nodes, &nw)

		for _, e := range n.Edges {
			sourceIdHash := nodeHash(n.ID)
			destIdHash := nodeHash(e.Dest.ID)
			protocol := ""
			if e.Metadata["protocol"] != nil {
				protocol = e.Metadata["protocol"].(string)
			}
			edgeId := edgeHash(sourceIdHash, destIdHash, protocol)
			ed := EdgeData{
				Id:     edgeId,
				Source: sourceIdHash,
				Target: destIdHash,
			}
			addEdgeTelemetry(&ed, e, o)

			ew := EdgeWrapper{
				Data: &ed,
			}
			*edges = append(*edges, &ew)
		}
	}
}

func addNodeTelemetry(s *graph.Node, nd *NodeData) {
	rate := getRate(s.Metadata, "rate")

	if rate > 0.0 {
		nd.Rate = fmt.Sprintf("%.2f", rate)

		rate3xx := getRate(s.Metadata, "rate3xx")
		rate4xx := getRate(s.Metadata, "rate4xx")
		rate5xx := getRate(s.Metadata, "rate5xx")

		if rate3xx > 0.0 {
			nd.Rate3xx = fmt.Sprintf("%.2f", rate3xx)
		}
		if rate4xx > 0.0 {
			nd.Rate4xx = fmt.Sprintf("%.2f", rate4xx)
		}
		if rate5xx > 0.0 {
			nd.Rate5xx = fmt.Sprintf("%.2f", rate5xx)
		}
	}

	rateOut := getRate(s.Metadata, "rateOut")

	if rateOut > 0.0 {
		nd.RateOut = fmt.Sprintf("%.2f", rateOut)
	}

	tcpSent := getRate(s.Metadata, "tcpSentRate")
	tcpSentOut := getRate(s.Metadata, "tcpSentRateOut")

	if tcpSent > 0.0 {
		nd.RateTcpSent = fmt.Sprintf("%.2f", tcpSent)
	}
	if tcpSentOut > 0.0 {
		nd.RateTcpSentOut = fmt.Sprintf("%.2f", tcpSentOut)
	}
}

func getRate(md map[string]interface{}, k string) float64 {
	if rate, ok := md[k]; ok {
		return rate.(float64)
	}
	return 0.0
}

func addEdgeTelemetry(ed *EdgeData, e *graph.Edge, o options.VendorOptions) {
	rate := getRate(e.Metadata, "rate")

	if rate > 0.0 {
		rate3xx := getRate(e.Metadata, "rate3xx")
		rate4xx := getRate(e.Metadata, "rate4xx")
		rate5xx := getRate(e.Metadata, "rate5xx")
		rateErr := rate4xx + rate5xx
		percentErr := rateErr / rate * 100.0

		ed.Rate = fmt.Sprintf("%.2f", rate)
		if rate3xx > 0.0 {
			ed.Rate3xx = fmt.Sprintf("%.2f", rate3xx)
		}
		if rate4xx > 0.0 {
			ed.Rate4xx = fmt.Sprintf("%.2f", rate4xx)
		}
		if rate5xx > 0.0 {
			ed.Rate5xx = fmt.Sprintf("%.2f", rate5xx)
		}
		if percentErr > 0.0 {
			ed.PercentErr = fmt.Sprintf("%.2f", percentErr)
		}

		if val, ok := e.Metadata["responseTime"]; ok {
			responseTime := val.(float64)
			ed.ResponseTime = fmt.Sprintf("%.2f", responseTime)
		}

		percentRate := rate / getRate(e.Source.Metadata, "rateOut") * 100.0
		if percentRate < 100.0 {
			ed.PercentRate = fmt.Sprintf("%.2f", percentRate)
		}
	} else {
		if val, ok := e.Source.Metadata["isUnused"]; ok {
			ed.IsUnused = val.(bool)
		}
	}

	if val, ok := e.Metadata["isMTLS"]; ok {
		ed.IsMTLS = val.(bool)
	}

	tcpSentRate := getRate(e.Metadata, "tcpSentRate")
	if tcpSentRate > 0.0 {
		ed.TcpSentRate = fmt.Sprintf("%.2f", tcpSentRate)
	}
}

// groupByVersion adds compound nodes to group multiple versions of the same app
func groupByVersion(nodes *[]*NodeWrapper) {
	grouped := make(map[string][]*NodeData)

	for _, nw := range *nodes {
		if nw.Data.NodeType == graph.NodeTypeApp {
			k := fmt.Sprintf("box_%s_%s", nw.Data.Namespace, nw.Data.App)
			grouped[k] = append(grouped[k], nw.Data)
		}
	}

	for k, members := range grouped {
		if len(members) > 1 {
			// create the compound grouping all versions of the app
			nodeId := nodeHash(k)
			nd := NodeData{
				Id:        nodeId,
				NodeType:  graph.NodeTypeApp,
				Namespace: members[0].Namespace,
				App:       members[0].App,
				Version:   "",
				IsGroup:   options.GroupByVersion,
			}

			nw := NodeWrapper{
				Data: &nd,
			}

			// assign each app version node to the compound parent
			nd.HasMissingSC = false // TODO: this is probably unecessarily noisy
			nd.IsInaccessible = false
			nd.IsOutside = false

			for _, n := range members {
				n.Parent = nodeId

				// copy some member attributes to to the compound node (aka app box)
				nd.HasMissingSC = nd.HasMissingSC || n.HasMissingSC
				nd.IsInaccessible = nd.IsInaccessible || n.IsInaccessible
				nd.IsOutside = nd.IsOutside || n.IsOutside
			}

			// add the compound node to the list of nodes
			*nodes = append(*nodes, &nw)
		}
	}
}
