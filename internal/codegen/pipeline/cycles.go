package pipeline

import (
	"slices"
	"sort"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// ComputeBlockedImports builds the cross-namespace reference graph, detects
// import cycles (e.g. Windows.Foundation ↔ Windows.Foundation.Collections),
// and returns the edge set to sever: blocked[src][dst] means references
// from src to dst must degrade to raw types instead of importing. Edges are
// broken lowest-reference-count first (fewest degradations),
// deterministically — except default-interface embedding edges, which carry
// a large weight bonus because severing one demotes a whole runtime class
// (the class struct embeds its default interface).
func ComputeBlockedImports(registry *Registry) map[string]map[string]bool {
	const defaultEmbedWeight = 1000

	// Weighted edges: references from one namespace to another.
	edges := map[string]map[string]int{}
	for _, meta := range registry.Namespaces {
		weights := map[string]int{}
		count := func(ref *winrtmeta.TypeRef) {
			if ref.Kind != "Native" && ref.Namespace != "" && ref.Namespace != meta.Namespace {
				weights[ref.Namespace]++
			}
		}
		WalkNamespaceRefs(meta, count)
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.DefaultInterface != nil && class.DefaultInterface.Namespace != "" &&
				class.DefaultInterface.Namespace != meta.Namespace {
				weights[class.DefaultInterface.Namespace] += defaultEmbedWeight
			}
		}
		if len(weights) > 0 {
			edges[meta.Namespace] = weights
		}
	}

	blocked := map[string]map[string]bool{}
	for {
		cycle := findCycle(edges)
		if cycle == nil {
			return blocked
		}
		src, dst := lightestEdge(edges, cycle)
		delete(edges[src], dst)
		if blocked[src] == nil {
			blocked[src] = map[string]bool{}
		}
		blocked[src][dst] = true
	}
}

// WalkNamespaceRefs visits every TypeRef in a namespace (struct fields,
// interface requires/methods/properties/events, class interfaces incl. the
// default, and delegate Invoke signatures), recursing through generic
// arguments and array elements.
func WalkNamespaceRefs(meta *winrtmeta.NamespaceMeta, visit func(*winrtmeta.TypeRef)) {
	var walkRef func(*winrtmeta.TypeRef)
	walkRef = func(ref *winrtmeta.TypeRef) {
		if ref == nil {
			return
		}
		visit(ref)
		for i := range ref.Args {
			walkRef(&ref.Args[i])
		}
		walkRef(ref.Elem)
	}
	walkMethod := func(method *winrtmeta.Method) {
		for i := range method.Params {
			walkRef(&method.Params[i].Type)
		}
		walkRef(method.Return)
	}

	for name := range meta.Structs {
		definition := meta.Structs[name]
		for i := range definition.Fields {
			walkRef(&definition.Fields[i].Type)
		}
	}
	for name := range meta.Interfaces {
		definition := meta.Interfaces[name]
		for i := range definition.Requires {
			walkRef(&definition.Requires[i])
		}
		for i := range definition.Methods {
			walkMethod(&definition.Methods[i])
		}
		for i := range definition.Properties {
			walkRef(&definition.Properties[i].Type)
		}
		for i := range definition.Events {
			walkRef(&definition.Events[i].Type)
		}
	}
	for name := range meta.Classes {
		definition := meta.Classes[name]
		walkRef(definition.DefaultInterface)
		for i := range definition.Interfaces {
			walkRef(&definition.Interfaces[i])
		}
	}
	for name := range meta.Delegates {
		definition := meta.Delegates[name]
		walkMethod(&definition.Invoke)
	}
}

// findCycle DFSes the edge graph and returns one cycle as a node path
// (v0 → v1 → … → v0), or nil when the graph is acyclic. Iteration order is
// sorted for determinism.
func findCycle(edges map[string]map[string]int) []string {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var stack []string
	var cycle []string

	nodes := make([]string, 0, len(edges))
	for node := range edges {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	var visit func(node string) bool
	visit = func(node string) bool {
		state[node] = inStack
		stack = append(stack, node)
		targets := make([]string, 0, len(edges[node]))
		for target := range edges[node] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			switch state[target] {
			case inStack:
				// Slice the cycle out of the stack.
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == target {
						cycle = append(slices.Clone(stack[i:]), target)
						return true
					}
				}
			case unvisited:
				if visit(target) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = done
		return false
	}
	for _, node := range nodes {
		if state[node] == unvisited && visit(node) {
			return cycle
		}
	}
	return nil
}

// lightestEdge picks the cycle edge with the fewest references (cheapest to
// degrade), breaking ties by name for determinism.
func lightestEdge(edges map[string]map[string]int, cycle []string) (string, string) {
	bestSrc, bestDst := cycle[0], cycle[1]
	bestWeight := edges[bestSrc][bestDst]
	for i := range len(cycle) - 1 {
		src, dst := cycle[i], cycle[i+1]
		weight := edges[src][dst]
		if weight < bestWeight || (weight == bestWeight && src+"→"+dst < bestSrc+"→"+bestDst) {
			bestSrc, bestDst, bestWeight = src, dst, weight
		}
	}
	return bestSrc, bestDst
}
