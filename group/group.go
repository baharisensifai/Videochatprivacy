package group

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v2"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/token"
)

var Directory, DataDirectory string
var UseMDNS bool
var UDPMin, UDPMax uint16

var ErrNotAuthorised = errors.New("not authorised")
var ErrAnonymousNotAuthorised = errors.New("anonymous users not authorised in this group")

type UserError string

func (err UserError) Error() string {
	return string(err)
}

type KickError struct {
	Id       string
	Username string
	Message  string
}

func (err KickError) Error() string {
	m := "kicked out"
	if err.Message != "" {
		m += " (" + err.Message + ")"
	}
	if err.Username != "" {
		m += " by " + err.Username
	}
	return m
}

type ProtocolError string

func (err ProtocolError) Error() string {
	return string(err)
}

type ChatHistoryEntry struct {
	Id    string
	User  string
	Time  int64
	Kind  string
	Value interface{}
}

const (
	LowBitrate = 100 * 1024
	MinBitrate = LowBitrate * 2
	MaxBitrate = 1024 * 1024 * 1024
)

type Group struct {
	name string

	mu          sync.Mutex
	description *Description
	locked      *string
	clients     map[string]Client
	history     []ChatHistoryEntry
	timestamp   time.Time
	data        map[string]interface{}
}

func (g *Group) Name() string {
	return g.name
}

func (g *Group) Locked() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locked != nil {
		return true, *g.locked
	} else {
		return false, ""
	}
}

func (g *Group) SetLocked(locked bool, message string) {
	g.mu.Lock()
	if locked {
		g.locked = &message
	} else {
		g.locked = nil
	}
	clients := g.getClientsUnlocked(nil)
	g.mu.Unlock()

	for _, c := range clients {
		c.Joined(g.Name(), "change")
	}
}

func (g *Group) Data() map[string]interface{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.data
}

func (g *Group) UpdateData(d map[string]interface{}) {
	g.mu.Lock()
	if g.data == nil {
		g.data = make(map[string]interface{})
	}
	for k, v := range d {
		if v == nil {
			delete(g.data, k)
		} else {
			g.data[k] = v
		}
	}
	clients := g.getClientsUnlocked(nil)
	g.mu.Unlock()

	for _, c := range clients {
		c.Joined(g.Name(), "change")
	}
}

func (g *Group) Description() *Description {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description
}

func (g *Group) ClientCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.clients)
}

func (g *Group) EmptyTime() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.clients) > 0 {
		return 0
	}
	return time.Since(g.timestamp)
}

var groups struct {
	mu     sync.Mutex
	groups map[string]*Group
}

func (g *Group) API() (*webrtc.API, error) {
	g.mu.Lock()
	codecs := g.description.Codecs
	g.mu.Unlock()

	return APIFromNames(codecs)
}

func fmtpValue(fmtp, key string) string {
	fields := strings.Split(fmtp, ";")
	for _, f := range fields {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

func CodecPayloadType(codec webrtc.RTPCodecCapability) (webrtc.PayloadType, error) {
	switch strings.ToLower(codec.MimeType) {
	case "video/vp8":
		return 96, nil
	case "video/vp9":
		profile := fmtpValue(codec.SDPFmtpLine, "profile-id")
		switch profile {
		case "0":
			return 98, nil
		case "2":
			return 100, nil
		default:
			return 0, errors.New("unknown VP9 profile")
		}
	case "video/av1":
		return 35, nil
	case "video/h264":
		profile := fmtpValue(codec.SDPFmtpLine, "profile-level-id")
		if len(profile) < 4 {
			return 0, errors.New("malforned H.264 profile")
		}
		switch strings.ToLower(profile[:4]) {
		case "4200":
			return 102, nil
		case "42e0":
			return 108, nil
		default:
			return 0, errors.New("unknown H.264 profile")
		}
	case "audio/opus":
		return 111, nil
	case "audio/g722":
		return 9, nil
	case "audio/pcmu":
		return 0, nil
	case "audio/pcma":
		return 8, nil
	default:
		return 0, errors.New("unknown codec")
	}
}

func codecsFromName(name string) ([]webrtc.RTPCodecParameters, error) {
	fb := []webrtc.RTCPFeedback{
		{"goog-remb", ""},
		{"nack", ""},
		{"nack", "pli"},
		{"ccm", "fir"},
	}

	var codecs []webrtc.RTPCodecCapability

	switch name {
	case "vp8":
		codecs = []webrtc.RTPCodecCapability{
			{
				"video/VP8", 90000, 0,
				"",
				fb,
			},
		}
	case "vp9":
		codecs = []webrtc.RTPCodecCapability{
			{
				"video/VP9", 90000, 0,
				"profile-id=0",
				fb,
			},
			{
				"video/VP9", 90000, 0,
				"profile-id=2",
				fb,
			},
		}
	case "av1":
		codecs = []webrtc.RTPCodecCapability{
			{
				"video/AV1", 90000, 0,
				"",
				fb,
			},
		}
	case "h264":
		codecs = []webrtc.RTPCodecCapability{
			{
				"video/H264", 90000, 0,
				"level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
				fb,
			},
		}
	case "opus":
		codecs = []webrtc.RTPCodecCapability{
			{
				"audio/opus", 48000, 2,
				"minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1",
				nil,
			},
		}
	case "g722":
		codecs = []webrtc.RTPCodecCapability{
			{
				"audio/G722", 8000, 1,
				"",
				nil,
			},
		}
	case "pcmu":
		codecs = []webrtc.RTPCodecCapability{
			{
				"audio/PCMU", 8000, 1,
				"",
				nil,
			},
		}
	case "pcma":
		codecs = []webrtc.RTPCodecCapability{
			{
				"audio/PCMU", 8000, 1,
				"",
				nil,
			},
		}
	default:
		return nil, errors.New("unknown codec")
	}

	parms := make([]webrtc.RTPCodecParameters, 0, len(codecs))
	for _, c := range codecs {
		ptype, err := CodecPayloadType(c)
		if err != nil {
			log.Printf("Couldn't determine ptype for codec %v: %v",
				c.MimeType, err)
			continue
		}
		parms = append(parms, webrtc.RTPCodecParameters{
			RTPCodecCapability: c,
			PayloadType:        ptype,
		})
	}
	return parms, nil
}

func APIFromCodecs(codecs []webrtc.RTPCodecParameters) (*webrtc.API, error) {
	s := webrtc.SettingEngine{}
	s.SetSRTPReplayProtectionWindow(512)
	if !UseMDNS {
		s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	}
	m := webrtc.MediaEngine{}

	for _, codec := range codecs {
		tpe := webrtc.RTPCodecTypeVideo
		if strings.HasPrefix(strings.ToLower(codec.MimeType), "audio/") {
			tpe = webrtc.RTPCodecTypeAudio
		}
		err := m.RegisterCodec(codec, tpe)
		if err != nil {
			log.Printf("%v", err)
			continue
		}
	}

	if UDPMin > 0 && UDPMax > 0 {
		s.SetEphemeralUDPPortRange(UDPMin, UDPMax)
	}
	m.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{sdp.SDESMidURI},
		webrtc.RTPCodecTypeVideo)
	m.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{sdp.SDESRTPStreamIDURI},
		webrtc.RTPCodecTypeVideo)

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(s),
		webrtc.WithMediaEngine(&m),
	), nil
}

func APIFromNames(names []string) (*webrtc.API, error) {
	if len(names) == 0 {
		names = []string{"vp8", "opus"}
	}
	var codecs []webrtc.RTPCodecParameters
	for _, n := range names {
		cs, err := codecsFromName(n)
		if err != nil {
			log.Printf("Codec %v: %v", n, err)
			continue
		}
		codecs = append(codecs, cs...)
	}

	return APIFromCodecs(codecs)
}

func Add(name string, desc *Description) (*Group, error) {
	g, notify, err := add(name, desc)
	for _, c := range notify {
		c.Joined(g.Name(), "change")
	}
	return g, err
}

func validGroupName(name string) bool {
	if filepath.Separator != '/' &&
		strings.ContainsRune(name, filepath.Separator) {
		return false
	}

	s := path.Clean("/" + name)
	if s == "/" {
		return false
	}

	return s == "/"+name
}

func add(name string, desc *Description) (*Group, []Client, error) {
	if !validGroupName(name) {
		return nil, nil, UserError("illegal group name")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	if groups.groups == nil {
		groups.groups = make(map[string]*Group)
	}

	var err error

	g := groups.groups[name]
	if g == nil {
		if desc == nil {
			desc, err = readDescription(name)
			if err != nil {
				return nil, nil, err
			}
		}

		g = &Group{
			name:        name,
			description: desc,
			clients:     make(map[string]Client),
			timestamp:   time.Now(),
		}
		groups.groups[name] = g
		return g, nil, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if desc != nil {
		if descriptionMatch(g.description, desc) {
			return g, nil, nil
		}
		g.description = desc
	} else if descriptionUnchanged(name, g.description) {
		return g, nil, nil
	} else {
		desc, err = readDescription(name)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Reading group %v: %v", name, err)
			}
			deleteUnlocked(g)
			return nil, nil, err
		}
		g.description = desc
	}

	clients := g.getClientsUnlocked(nil)
	autoLockKick(g, clients)

	return g, clients, nil
}

func Range(f func(g *Group) bool) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	for _, g := range groups.groups {
		ok := f(g)
		if !ok {
			break
		}
	}
}

func GetNames() []string {
	names := make([]string, 0)

	Range(func(g *Group) bool {
		names = append(names, g.name)
		return true
	})
	return names
}

type SubGroup struct {
	Name    string
	Clients int
}

func GetSubGroups(parent string) []SubGroup {
	prefix := parent + "/"
	subgroups := make([]SubGroup, 0)

	Range(func(g *Group) bool {
		if strings.HasPrefix(g.name, prefix) {
			g.mu.Lock()
			count := len(g.clients)
			g.mu.Unlock()
			if count > 0 {
				subgroups = append(subgroups,
					SubGroup{g.name, count})
			}
		}
		return true
	})
	return subgroups
}

func Get(name string) *Group {
	groups.mu.Lock()
	defer groups.mu.Unlock()
	if groups.groups == nil {
		return nil
	}
	return groups.groups[name]
}

func Delete(name string) bool {
	groups.mu.Lock()
	defer groups.mu.Unlock()
	g := groups.groups[name]
	if g == nil {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	return deleteUnlocked(g)
}

// Called with both groups.mu and g.mu taken.
func deleteUnlocked(g *Group) bool {
	if len(g.clients) != 0 {
		return false
	}

	delete(groups.groups, g.name)
	return true
}

func member(v string, l []string) bool {
	for _, w := range l {
		if v == w {
			return true
		}
	}
	return false
}

func AddClient(group string, c Client, creds ClientCredentials) (*Group, error) {
	g, err := Add(group, nil)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	clients := g.getClientsUnlocked(nil)

	if !member("system", c.Permissions()) {
		username, perms, err := g.getPermission(creds)
		if err != nil {
			return nil, err
		}

		c.SetUsername(username)
		c.SetPermissions(perms)

		if !member("op", perms) {
			if g.locked != nil {
				m := *g.locked
				if m == "" {
					m = "this group is locked"
				}
				return nil, UserError(m)
			}
			if g.description.Autokick {
				ops := false
				for _, c := range clients {
					if member("op", c.Permissions()) {
						ops = true
						break
					}
				}
				if !ops {
					return nil, UserError(
						"there are no operators " +
							"in this group",
					)
				}
			}
		}

		if !member("op", perms) && g.description.MaxClients > 0 {
			if len(g.clients) >= g.description.MaxClients {
				return nil, UserError("too many users")
			}
		}
	}

	if g.clients[c.Id()] != nil {
		return nil, ProtocolError("duplicate client id")
	}

	g.clients[c.Id()] = c
	g.timestamp = time.Now()

	c.Joined(g.Name(), "join")

	id := c.Id()
	u := c.Username()
	p := c.Permissions()
	s := c.Data()
	c.PushClient(g.Name(), "add", c.Id(), u, p, s)
	for _, cc := range clients {
		pp := cc.Permissions()
		c.PushClient(
			g.Name(), "add", cc.Id(), cc.Username(), pp, cc.Data(),
		)
		cc.PushClient(g.Name(), "add", id, u, p, s)
	}

	return g, nil
}

// called locked
func autoLockKick(g *Group, clients []Client) {
	if !(g.description.Autolock && g.locked == nil) &&
		!g.description.Autokick {
		return
	}
	for _, c := range clients {
		if member("op", c.Permissions()) {
			return
		}
	}
	if g.description.Autolock && g.locked == nil {
		m := "this group is locked"
		g.locked = &m
		go func(clients []Client) {
			for _, c := range clients {
				c.Joined(g.Name(), "change")
			}
		}(g.getClientsUnlocked(nil))
	}

	if g.description.Autokick {
		go kickall(g, "there are no operators in this group")
	}
}

func DelClient(c Client) {
	g := c.Group()
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.clients[c.Id()] != c {
		log.Printf("Deleting unknown client")
		g.mu.Unlock()
		return
	}
	delete(g.clients, c.Id())
	g.timestamp = time.Now()
	clients := g.getClientsUnlocked(nil)
	g.mu.Unlock()

	c.Joined(g.Name(), "leave")
	for _, cc := range clients {
		cc.PushClient(
			g.Name(), "delete", c.Id(), "", nil, nil,
		)
	}
	autoLockKick(g, clients)
}

func (g *Group) GetClients(except Client) []Client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientsUnlocked(except)
}

func (g *Group) getClientsUnlocked(except Client) []Client {
	clients := make([]Client, 0, len(g.clients))
	for _, c := range g.clients {
		if c != except {
			clients = append(clients, c)
		}
	}
	return clients
}

func (g *Group) GetClient(id string) Client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientUnlocked(id)
}

func (g *Group) getClientUnlocked(id string) Client {
	for idd, c := range g.clients {
		if idd == id {
			return c
		}
	}
	return nil
}

func (g *Group) Range(f func(c Client) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		ok := f(c)
		if !ok {
			break
		}
	}
}

func kickall(g *Group, message string) {
	g.Range(func(c Client) bool {
		c.Kick("", "", message)
		return true
	})
}

func Shutdown(message string) {
	Range(func(g *Group) bool {
		g.SetLocked(true, message)
		kickall(g, message)
		return true
	})
}

type warner interface {
	Warn(oponly bool, message string) error
}

func (g *Group) WallOps(message string) {
	clients := g.GetClients(nil)
	for _, c := range clients {
		w, ok := c.(warner)
		if !ok {
			continue
		}
		err := w.Warn(true, message)
		if err != nil {
			log.Printf("WallOps: %v", err)
		}
	}
}

func FromJSTime(tm int64) time.Time {
	if tm == 0 {
		return time.Time{}
	}
	return time.Unix(int64(tm)/1000, (int64(tm)%1000)*1000000)
}

func ToJSTime(tm time.Time) int64 {
	return int64((tm.Sub(time.Unix(0, 0)) + time.Millisecond/2) /
		time.Millisecond)
}

const maxChatHistory = 50

func (g *Group) ClearChatHistory() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.history = nil
}

func (g *Group) AddToChatHistory(id, user string, time int64, kind string, value interface{}) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.history) >= maxChatHistory {
		copy(g.history, g.history[1:])
		g.history = g.history[:len(g.history)-1]
	}
	g.history = append(g.history,
		ChatHistoryEntry{Id: id, User: user, Time: time, Kind: kind, Value: value},
	)
}

func discardObsoleteHistory(h []ChatHistoryEntry, duration time.Duration) []ChatHistoryEntry {
	i := 0
	for i < len(h) {
		if time.Since(FromJSTime(h[i].Time)) <= duration {
			break
		}
		i++
	}
	if i > 0 {
		copy(h, h[i:])
		h = h[:len(h)-i]
	}
	return h
}

func (g *Group) GetChatHistory() []ChatHistoryEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.history = discardObsoleteHistory(
		g.history, maxHistoryAge(g.description),
	)

	h := make([]ChatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
}

func matchClient(creds ClientCredentials, users []ClientPattern) (bool, bool) {
	matched := false
	for _, u := range users {
		if u.Username == creds.Username {
			matched = true
			if u.Password == nil {
				return true, true
			}
			m, _ := u.Password.Match(creds.Password)
			if m {
				return true, true
			}
		}
	}
	if matched {
		return true, false
	}

	for _, u := range users {
		if u.Username == "" {
			if u.Password == nil {
				return true, true
			}
			m, _ := u.Password.Match(creds.Password)
			if m {
				return true, true
			}
		}
	}
	return false, false
}

// Configuration represents the contents of the data/config.json file.
type Configuration struct {
	// The modtime and size of the file.  These are used to detect
	// when a file has changed on disk.
	modTime  time.Time `json:"-"`
	fileSize int64     `json:"-"`

	CanonicalHost string          `json:"canonicalHost"`
	Admin         []ClientPattern `json:"admin"`
}

func (conf Configuration) Zero() bool {
	return conf.modTime.Equal(time.Time{}) &&
		conf.fileSize == 0
}

var configuration struct {
	mu            sync.Mutex
	configuration *Configuration
}

func GetConfiguration() (*Configuration, error) {
	configuration.mu.Lock()
	defer configuration.mu.Unlock()

	if configuration.configuration == nil {
		configuration.configuration = &Configuration{}
	}

	filename := filepath.Join(DataDirectory, "config.json")
	fi, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			if !configuration.configuration.Zero() {
				configuration.configuration = &Configuration{}
			}
			return configuration.configuration, nil
		}
		return nil, err
	}

	if configuration.configuration.modTime.Equal(fi.ModTime()) &&
		configuration.configuration.fileSize == fi.Size() {
		return configuration.configuration, nil
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	var conf Configuration
	err = d.Decode(&conf)
	if err != nil {
		return nil, err
	}
	configuration.configuration = &conf
	return configuration.configuration, nil
}

// Description represents a group description together with some metadata
// about the JSON file it was deserialised from.
type Description struct {
	// The file this was deserialised from.  This is not necessarily
	// the name of the group, for example in case of a subgroup.
	FileName string `json:"-"`

	// The modtime and size of the file.  These are used to detect
	// when a file has changed on disk.
	modTime  time.Time `json:"-"`
	fileSize int64     `json:"-"`

	// The user-friendly group name
	DisplayName string `json:"displayName,omitempty"`

	// A user-readable description of the group.
	Description string `json:"description,omitempty"`

	// A user-readable contact, typically an e-mail address.
	Contact string `json:"contact,omitempty"`

	// A user-readable comment.  Ignored by the server.
	Comment string `json:"comment,omitempty"`

	// Whether to display the group on the landing page.
	Public bool `json:"public,omitempty"`

	// A URL to redirect the group to.  If this is not empty, most
	// other fields are ignored.
	Redirect string `json:"redirect,omitempty"`

	// The maximum number of simultaneous clients.  Unlimited if 0.
	MaxClients int `json:"max-clients,omitempty"`

	// The time for which history entries are kept.
	MaxHistoryAge int `json:"max-history-age,omitempty"`

	// Whether users are allowed to log in with an empty username.
	AllowAnonymous bool `json:"allow-anonymous,omitempty"`

	// Whether recording is allowed.
	AllowRecording bool `json:"allow-recording,omitempty"`

	// Whether subgroups are created on the fly.
	AllowSubgroups bool `json:"allow-subgroups,omitempty"`

	// Whether to lock the group when the last op logs out.
	Autolock bool `json:"autolock,omitempty"`

	// Whether to kick all users when the last op logs out.
	Autokick bool `json:"autokick,omitempty"`

	// A list of logins for ops.
	Op []ClientPattern `json:"op,omitempty"`

	// A list of logins for presenters.
	Presenter []ClientPattern `json:"presenter,omitempty"`

	// A list of logins for non-presenting users.
	Other []ClientPattern `json:"other,omitempty"`

	// The (public) keys used for token authentication.
	AuthKeys []map[string]interface{} `json:"authKeys"`

	// The URL of the authentication server, if any.
	AuthServer string `json:"authServer"`

	// The URL of the authentication portal, if any.
	AuthPortal string `json:"authPortal"`

	// Codec preferences.  If empty, a suitable default is chosen in
	// the APIFromNames function.
	Codecs []string `json:"codecs,omitempty"`
}

const DefaultMaxHistoryAge = 4 * time.Hour

func maxHistoryAge(desc *Description) time.Duration {
	if desc.MaxHistoryAge != 0 {
		return time.Duration(desc.MaxHistoryAge) * time.Second
	}
	return DefaultMaxHistoryAge
}

func openDescriptionFile(name string) (*os.File, string, bool, error) {
	isParent := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		r, err := os.Open(fileName)
		if !os.IsNotExist(err) {
			return r, fileName, isParent, err
		}
		isParent = true
		name, _ = path.Split(name)
		name = strings.TrimRight(name, "/")
	}
	return nil, "", false, os.ErrNotExist
}

func statDescriptionFile(name string) (os.FileInfo, string, bool, error) {
	isParent := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		fi, err := os.Stat(fileName)
		if !os.IsNotExist(err) {
			return fi, fileName, isParent, err
		}
		isParent = true
		name, _ = path.Split(name)
		name = strings.TrimRight(name, "/")
	}
	return nil, "", false, os.ErrNotExist
}

// descriptionMatch returns true if the description hasn't changed between
// d1 and d2
func descriptionMatch(d1, d2 *Description) bool {
	if d1.FileName != d2.FileName {
		return false
	}

	if d1.fileSize != d2.fileSize || !d1.modTime.Equal(d2.modTime) {
		return false
	}
	return true
}

// descriptionUnchanged returns true if a group's description hasn't
// changed since it was last read.
func descriptionUnchanged(name string, desc *Description) bool {
	fi, fileName, _, err := statDescriptionFile(name)
	if err != nil || fileName != desc.FileName {
		return false
	}

	if fi.Size() != desc.fileSize || !fi.ModTime().Equal(desc.modTime) {
		return false
	}
	return true
}

// GetDescription gets a group description, either from cache or from disk
func GetDescription(name string) (*Description, error) {
	g := Get(name)
	if g != nil {
		if descriptionUnchanged(name, g.description) {
			return g.description, nil
		}
	}

	return readDescription(name)
}

// readDescription reads a group's description from disk
func readDescription(name string) (*Description, error) {
	r, fileName, isParent, err := openDescriptionFile(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var desc Description

	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	err = d.Decode(&desc)
	if err != nil {
		return nil, err
	}
	if isParent {
		if !desc.AllowSubgroups {
			return nil, os.ErrNotExist
		}
		desc.Public = false
		desc.Description = ""
	}

	desc.FileName = fileName
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()

	return &desc, nil
}

// called locked
func (g *Group) getPermission(creds ClientCredentials) (string, []string, error) {
	desc := g.description
	if creds.Token == "" {
		if !desc.AllowAnonymous && creds.Username == "" {
			return "", nil, ErrAnonymousNotAuthorised
		}
		if found, good := matchClient(creds, desc.Op); found {
			if good {
				var p []string
				p = []string{"op", "present"}
				if desc.AllowRecording {
					p = append(p, "record")
				}
				return creds.Username, p, nil
			}
			return "", nil, ErrNotAuthorised
		}
		if found, good := matchClient(creds, desc.Presenter); found {
			if good {
				return creds.Username, []string{"present"}, nil
			}
			return "", nil, ErrNotAuthorised
		}
		if found, good := matchClient(creds, desc.Other); found {
			if good {
				return creds.Username, nil, nil
			}
			return "", nil, ErrNotAuthorised
		}
		return "", nil, ErrNotAuthorised
	}

	sub, aud, perms, err := token.Valid(creds.Token, desc.AuthKeys)
	if err != nil {
		log.Printf("Token authentication: %v", err)
		return "", nil, ErrNotAuthorised
	}
	if !desc.AllowAnonymous && sub == "" {
		return "", nil, ErrAnonymousNotAuthorised
	}
	conf, err := GetConfiguration()
	if err != nil {
		log.Printf("Read config.json: %v", err)
		return "", nil, err
	}
	ok := false
	for _, u := range aud {
		url, err := url.Parse(u)
		if err != nil {
			log.Printf("Token URL: %v", err)
			continue
		}
		// if canonicalHost is not set, we allow tokens
		// for any domain name.  Hopefully different
		// servers use distinct keys.
		if conf.CanonicalHost != "" {
			if !strings.EqualFold(
				url.Host, conf.CanonicalHost,
			) {
				continue
			}
		}
		if url.Path == path.Join("/group", g.name)+"/" {
			ok = true
			break
		}
	}
	if !ok {
		return "", nil, ErrNotAuthorised
	}
	return sub, perms, nil
}

func (g *Group) GetPermission(creds ClientCredentials) (string, []string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getPermission(creds)
}

type Status struct {
	Name        string `json:"name"`
	Redirect    string `json:"redirect,omitempty"`
	Location    string `json:"location,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	AuthServer  string `json:"authServer,omitempty"`
	AuthPortal  string `json:"authPortal,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
	ClientCount *int   `json:"clientCount,omitempty"`
}

// Status returns a group's status.
// Base is the base URL for groups; if omitted, then both the Location and
// Endpoint members are omitted from the result.
func (g *Group) Status(authentified bool, base string) Status {
	desc := g.Description()

	if desc.Redirect != "" {
		return Status{
			Name:        g.name,
			Redirect:    desc.Redirect,
			DisplayName: desc.DisplayName,
			Description: desc.Description,
		}
	}

	var location, endpoint string
	if base != "" {
		burl, err := url.Parse(base)
		if err == nil {
			wss := "wss"
			if burl.Scheme == "http"{
			//if (burl.Scheme == "http" || burl.Scheme == "") {
				wss = "ws"
			}
			l := url.URL{
				Scheme: burl.Scheme,
				Host:   burl.Host,
				Path:   path.Join(burl.Path, g.name) + "/",
			}
			location = l.String()
			e := url.URL{
				Scheme: wss,
				Host:   burl.Host,
				Path:   "/ws",
			}
			endpoint = e.String()
		} else {
			log.Printf("Couldn't parse base URL %v", base)
		}
	}

	d := Status{
		Name:        g.name,
		Location:    location,
		Endpoint:    endpoint,
		DisplayName: desc.DisplayName,
		AuthServer:  desc.AuthServer,
		AuthPortal:  desc.AuthPortal,
		Description: desc.Description,
	}

	if authentified || desc.Public {
		// these are considered private information
		locked, _ := g.Locked()
		count := g.ClientCount()
		d.Locked = locked
		d.ClientCount = &count
	}
	return d
}

func GetPublic(base string) []Status {
	gs := make([]Status, 0)
	Range(func(g *Group) bool {
		if g.Description().Public {
			gs = append(gs, g.Status(false, base))
		}
		return true
	})
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].Name < gs[j].Name
	})
	return gs
}

// Update checks that all in-memory groups are up-to-date and updates the
// list of public groups.  It also removes from memory any non-public
// groups that haven't been accessed in maxHistoryAge.
func Update() {
	_, err := GetConfiguration()
	if err != nil {
		log.Printf("%v: %v",
			filepath.Join(DataDirectory, "config.json"),
			err,
		)
	}

	names := GetNames()

	for _, name := range names {
		g := Get(name)
		if g == nil {
			continue
		}

		deleted := false
		historyAge := maxHistoryAge(g.description)
		if !g.description.Public && g.EmptyTime() > historyAge {
			// Delete checks if the group is still empty
			deleted = Delete(name)
		}

		if !deleted && !descriptionUnchanged(name, g.description) {
			Add(name, nil)
		}
	}

	err = filepath.WalkDir(
		Directory,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			filename, err := filepath.Rel(Directory, path)
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if !strings.HasSuffix(filename, ".json") {
				log.Printf(
					"Unexpected extension for group file %v",
					path,
				)
				return nil
			}
			base := filepath.Base(filename)
			if base[0] == '.' {
				log.Printf("Group file %v ignored", filename)
				return nil
			}
			name := strings.TrimSuffix(filename, ".json")
			desc, err := GetDescription(name)
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if desc.Public {
				Add(name, desc)
			}
			return nil
		},
	)

	if err != nil {
		log.Printf("Couldn't read groups: %v", err)
	}
}
