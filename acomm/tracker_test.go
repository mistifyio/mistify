package acomm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/mistifyio/mistify/acomm"
	"github.com/stretchr/testify/suite"
)

type TrackerTestSuite struct {
	suite.Suite
	Tracker      *acomm.Tracker
	RespServer   *httptest.Server
	StreamServer *httptest.Server
	Responses    chan *acomm.Response
	Request      *acomm.Request
}

func (s *TrackerTestSuite) SetupSuite() {
	log.SetLevel(log.FatalLevel)
	s.Responses = make(chan *acomm.Response, 10)

	// Mock HTTP response server
	s.RespServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &acomm.Response{}
		body, err := ioutil.ReadAll(r.Body)
		s.NoError(err, "should not fail reading body")
		s.NoError(json.Unmarshal(body, resp), "should not fail unmarshalling response")
		s.Responses <- resp
	}))

	// Mock HTTP Stream server
	s.StreamServer = httptest.NewServer(http.HandlerFunc(acomm.ProxyStreamHandler))
}

func (s *TrackerTestSuite) SetupTest() {
	var err error

	s.Request, err = acomm.NewRequest("foobar", s.RespServer.URL, "", nil, nil, nil)
	s.Require().NoError(err, "request should be valid")

	streamAddr, _ := url.ParseRequestURI(s.StreamServer.URL)
	s.Tracker, err = acomm.NewTracker("", streamAddr, 0)
	s.Require().NoError(err, "failed to create new Tracker")
	s.Require().NotNil(s.Tracker, "failed to create new Tracker")
}

func (s *TrackerTestSuite) TearDownTest() {
	s.Tracker.Stop()
}

func (s *TrackerTestSuite) TearDownSuite() {
	s.RespServer.Close()
	s.StreamServer.Close()
}

func TestTrackerTestSuite(t *testing.T) {
	suite.Run(t, new(TrackerTestSuite))
}

func (s *TrackerTestSuite) NextResp() *acomm.Response {
	return nextResp(s.Responses)
}

func nextResp(respChan chan *acomm.Response) *acomm.Response {
	var resp *acomm.Response
	select {
	case resp = <-respChan:
	case <-time.After(5 * time.Second):
	}
	return resp
}

func (s *TrackerTestSuite) TestTrackRequest() {
	if !s.NoError(s.Tracker.Start(), "should have started tracker") {
		return
	}
	s.NoError(s.Tracker.TrackRequest(s.Request, 0), "should have successfully tracked request")
	s.Error(s.Tracker.TrackRequest(s.Request, 0), "duplicate ID should have failed to track request")
	s.Equal(1, s.Tracker.NumRequests(), "should have one unique request tracked")
	s.True(s.Tracker.RemoveRequest(s.Request))
	s.Equal(0, s.Tracker.NumRequests(), "should have one request tracked")

	s.NoError(s.Tracker.TrackRequest(s.Request, 500*time.Millisecond), "should have successfully tracked request with timeout")
	s.Equal(1, s.Tracker.NumRequests(), "should have one request tracked")
	time.Sleep(time.Second)
	s.Equal(0, s.Tracker.NumRequests(), "timeout should have removed request")
}

func (s *TrackerTestSuite) TestStartListener() {
	s.NoError(s.Tracker.Start(), "starting an unstarted should not error")
	s.NoError(s.Tracker.Start(), "starting an started should not error")

	s.NoError(s.Tracker.TrackRequest(s.Request, 0), "should have successfully tracked request")

	go s.Tracker.RemoveRequest(s.Request)
}

func (s *TrackerTestSuite) TestProxyUnix() {
	unixReq, err := s.Tracker.ProxyUnix(s.Request, 0)
	s.Error(err, "should fail to proxy when tracker is not listening")
	s.Nil(unixReq, "should not return a request")

	if !s.NoError(s.Tracker.Start(), "listner should start") {
		return
	}

	streamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	defer streamServer.Close()

	req, err := acomm.NewRequest("foobar", s.RespServer.URL, streamServer.URL, nil, nil, nil)
	s.NoError(err)

	unixReq, err = s.Tracker.ProxyUnix(req, 0)
	s.NoError(err, "should not fail proxying when tracker is listening")
	s.NotNil(unixReq, "should return a request")
	s.Equal(req.ID, unixReq.ID, "new request should share ID with original")
	s.Equal("unix", unixReq.ResponseHook.Scheme, "new request should have a unix response hook")
	s.Equal(1, s.Tracker.NumRequests(), "should have tracked the new request")

	var reqStreamData bytes.Buffer
	s.NoError(acomm.Stream(&reqStreamData, unixReq.StreamURL), "should have streamed req data")
	s.Equal("hello", reqStreamData.String(), "should have streamed req data")

	resp, err := acomm.NewResponse(unixReq, struct{}{}, nil, nil)
	if !s.NoError(err, "new response should not error") {
		return
	}
	if !s.NoError(acomm.Send(unixReq.ResponseHook, resp), "response send should not error") {
		return
	}

	lastResp := s.NextResp()
	if !s.NotNil(lastResp, "response should have been proxied to original http response hook") {
		return
	}
	s.Equal(resp.ID, lastResp.ID, "response should have been proxied to original http response hook")
	s.Equal(0, s.Tracker.NumRequests(), "should have removed the request from tracking")

	// Should not proxy a request already using unix response hook
	origUnixReq, err := acomm.NewRequest("foobar", "unix://foo", "", struct{}{}, nil, nil)
	if !s.NoError(err, "new request shoudl not error") {
		return
	}
	unixReq, err = s.Tracker.ProxyUnix(origUnixReq, 0)
	s.NoError(err, "should not error with unix response hook")
	s.Equal(origUnixReq, unixReq, "should not proxy unix response hook")
	s.Equal(0, s.Tracker.NumRequests(), "should not response an unproxied request")
}
