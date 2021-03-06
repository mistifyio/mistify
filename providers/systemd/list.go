package systemd

import (
	"net/url"

	"github.com/coreos/go-systemd/dbus"
	"github.com/mistifyio/mistify/acomm"
)

// ListResult is the result of the List handler.
type ListResult struct {
	Units []dbus.UnitStatus `json:"units"`
}

// List retuns a list of unit statuses.
func (s *Systemd) List(req *acomm.Request) (interface{}, *url.URL, error) {
	list, err := s.dconn.ListUnits()
	return &ListResult{list}, nil, err
}
