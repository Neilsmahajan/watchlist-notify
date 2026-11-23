package watchlist

import (
	"reflect"
	"testing"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

func TestGroupByService(t *testing.T) {
	f := newFixture()
	// Every item, newest first: Matrix, Thrones, Arrival, Unlinked, Missing.
	groups := GroupByService(f.list(t, Options{}).Items)

	got := map[string][]string{}
	var order []string
	for _, group := range groups {
		order = append(order, group.Name)
		for _, item := range group.Items {
			got[group.Code] = append(got[group.Code], item.Title)
		}
	}
	// Arrival is on both of the viewer's services and listed under both.
	// Thrones is only on a service the viewer does not have; Unlinked and
	// Missing have no availability at all.
	want := map[string][]string{
		models.ServiceHulu:    {"Arrival"},
		models.ServiceNetflix: {"The Matrix", "Arrival"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(order, []string{"Hulu", "Netflix"}) {
		t.Errorf("order = %v, want services by name", order)
	}
}

func TestGroupByServiceWithNothingStreaming(t *testing.T) {
	f := newFixture()
	f.user.Services = nil
	if groups := GroupByService(f.list(t, Options{}).Items); len(groups) != 0 {
		t.Errorf("groups = %+v, want none for a viewer without services", groups)
	}
	if groups := GroupByService(nil); len(groups) != 0 {
		t.Errorf("groups = %+v, want none for no items", groups)
	}
}
