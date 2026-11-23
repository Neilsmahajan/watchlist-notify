package watchlist

import "sort"

// ServiceGroup is one of the user's services with the items it streams.
type ServiceGroup struct {
	Code  string // our service code
	Name  string
	Items []*Item
}

// GroupByService arranges items under each of the user's services that
// streams them: the answer to "what can I watch, and where?".
//
// An item streaming on two of the user's services is listed under both. An
// item on none of them is left out, and so is a service that streams none of
// the items. Groups are ordered by service name; items keep the order they
// came in.
func GroupByService(items []Item) []ServiceGroup {
	index := make(map[string]int)
	var groups []ServiceGroup
	for i := range items {
		item := &items[i]
		if item.Availability == nil {
			continue
		}
		// Providers holds only the user's active services that stream the title.
		for _, provider := range item.Availability.Providers {
			at, ok := index[provider.Code]
			if !ok {
				at = len(groups)
				index[provider.Code] = at
				groups = append(groups, ServiceGroup{Code: provider.Code, Name: provider.Name})
			}
			groups[at].Items = append(groups[at].Items, item)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}
