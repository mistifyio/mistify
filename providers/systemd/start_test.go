package systemd_test

import (
	"fmt"

	"github.com/mistifyio/mistify/acomm"
	"github.com/mistifyio/mistify/providers/systemd"
)

func (s *sd) TestStart() {
	exit1Svc := "systemd-test-fail.service"
	s.Require().NoError(enable(exit1Svc))
	defer func() {
		s.Require().NoError(disable(exit1Svc))
	}()

	tests := []struct {
		name string
		mode string
		err  string
	}{
		{"", "", "missing arg: name"},
		{"doesnotexist.service", systemd.ModeFail, "unit not found"},
		{"dbus.service", systemd.ModeFail, ""},
		{exit1Svc, systemd.ModeFail, ""},
	}

	for _, test := range tests {
		args := &systemd.StartArgs{test.name, test.mode}
		argsS := fmt.Sprintf("%+v", test)

		req, err := acomm.NewRequest("zfs-exists", "unix:///tmp/foobar", "", args, nil, nil)
		s.Require().NoError(err, argsS)

		res, streamURL, err := s.systemd.Start(req)
		s.Nil(streamURL, argsS)
		if test.err == "" {
			if !s.NoError(err, argsS) {
				continue
			}
			result, ok := res.(*systemd.StartResult)
			if !s.True(ok, argsS) {
				continue
			}
			s.NotEmpty(result.JobID, argsS)
		} else {
			s.EqualError(err, test.err, argsS)
		}
	}
}
