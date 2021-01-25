package matrix

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/42wim/matterircd/bridge"
	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/viper"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type Matrix struct {
	mc          *mautrix.Client
	credentials bridge.Credentials
	quitChan    []chan struct{}
	eventChan   chan *bridge.Event
	v           *viper.Viper
	connected   bool
	firstSync   bool
	dmChannels  map[id.RoomID][]id.UserID
	channels    map[id.RoomID]*Channel
	users       map[id.UserID]*User
	sync.RWMutex
}

func New(v *viper.Viper, cred bridge.Credentials, eventChan chan *bridge.Event, onWsConnect func()) (bridge.Bridger, *mautrix.Client, error) {
	m := &Matrix{
		credentials: cred,
		eventChan:   eventChan,
		v:           v,
		channels:    make(map[id.RoomID]*Channel),
		dmChannels:  make(map[id.RoomID][]id.UserID),
		users:       make(map[id.UserID]*User),
	}

	mc, err := mautrix.NewClient(cred.Server, "", "")
	if err != nil {
		return nil, nil, err
	}

	resp, err := mc.Login(&mautrix.ReqLogin{
		Type: "m.login.password",
		Identifier: mautrix.UserIdentifier{
			Type: "m.id.user",
			User: cred.Login,
		},
		Password: cred.Pass,
	})
	if err != nil {
		return nil, nil, err
	}

	mc.SetCredentials(resp.UserID, resp.AccessToken)

	m.mc = mc

	m.handleMatrix(onWsConnect)

	return m, mc, nil
}

func (m *Matrix) syncCallback(resp *mautrix.RespSync, since string) bool {
	m.firstSync = true

	return true
}

func (m *Matrix) handleMatrix(onConnect func()) {
	syncer := m.mc.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnSync(m.syncCallback)

	/*
		syncer.OnEventType(event.EventRedaction, m.handleEvent)
		syncer.OnEventType(event.EventMessage, m.handleEvent)
		syncer.OnEventType(event.StateMember, m.handleMember)
		syncer.OnEventType(event.StateCreate, m.handleCreate)
		syncer.OnEventType(event.AccountDataDirectChats, m.handleDM)
		syncer.OnEventType(event.StateCanonicalAlias, m.handleCanonicalAlias)
		syncer.OnEventType(event.StateRoomName, m.handleRoomName)

	*/
	/*
		syncer.OnEvent(func(source mautrix.EventSource, evt *event.Event) {
			fmt.Println(source.String())
			spew.Dump(evt)
		})
	*/
	//syncer.OnEventType(event.StateMember, m.handleMemberChange)

	/*
		resp, err := m.mc.SyncRequest(30000, "", "", true, "")
		if err != nil {
			return
		}

		fmt.Println("resp length:", len(resp.Rooms.Join))

		for room, sync := range resp.Rooms.Join {
			fmt.Println(room)

			for _, ev := range append(append(
				append(sync.State.Events, sync.Timeline.Events...),
				sync.Ephemeral.Events...),
				sync.AccountData.Events...) {
				ev.Content.ParseRaw(ev.Type)
				ev.RoomID = room
				spew.Dump(ev)

				switch ev.Type {
				case event.StateCanonicalAlias:
					m.handleCanonicalAlias(mautrix.EventSourceState, ev)
				case event.StateRoomName:
					m.handleRoomName(mautrix.EventSourceState, ev)
				case event.StateMember:
					m.handleMember(mautrix.EventSourceState, ev)
				case event.AccountDataDirectChats:
					m.handleDM(mautrix.EventSourceAccountData, ev)
				}
			}
			//spew.Dump(ev)
			//		fmt.Printf("%#v", event)
			//break
		}
	*/

	/*

		resp, err = m.mc.SyncRequest(30000, resp.NextBatch, "", false, "")
		if err != nil {
			return
		}

		fmt.Println("resp length:", len(resp.Rooms.Join))

		for room, sync := range resp.Rooms.Join {
			fmt.Println(room)
			for _, ev := range sync.State.Events {
				ev.Content.ParseRaw(ev.Type)
				spew.Dump(ev)

				switch ev.Type {
				case event.StateCanonicalAlias:
					m.handleCanonicalAlias(mautrix.EventSourceState, ev)
				case event.StateRoomName:
					m.handleRoomName(mautrix.EventSourceState, ev)
				case event.StateMember:
					m.handleMember(mautrix.EventSourceState, ev)
				case event.AccountDataDirectChats:
					m.handleDM(mautrix.EventSourceAccountData, ev)
				}
			}
			//spew.Dump(ev)
			//		fmt.Printf("%#v", event)
			//break
		}
	*/

	fmt.Println("dumping")
	//	spew.Dump(resp)

	syncer.OnEventType(event.EventRedaction, m.handleEvent)
	syncer.OnEventType(event.EventMessage, m.handleEvent)
	syncer.OnEventType(event.StateMember, m.handleMember)
	syncer.OnEventType(event.StateCreate, m.handleCreate)
	syncer.OnEventType(event.StateRoomName, m.handleRoomName)
	syncer.OnEventType(event.AccountDataDirectChats, m.handleDM)
	syncer.OnEventType(event.StateCanonicalAlias, m.handleCanonicalAlias)
	syncer.OnEvent(func(source mautrix.EventSource, evt *event.Event) {
		fmt.Println(source.String())
		spew.Dump(evt)
	})

	//spew.Dump(m.channels)
	//spew.Dump(m.users)

	//syncer.OnEventType(event.StateMember, m.handleMember)
	syncer.OnEventType(event.EventRedaction, m.handleEvent)

	go func() {
		for {
			if err := m.mc.Sync(); err != nil {
				log.Println("Sync() returned ", err)
			}
		}
	}()

	for m.firstSync == false {
		fmt.Println("syncing..")
		time.Sleep(time.Second)
	}

	fmt.Println("sync complete")

	go onConnect()
}

func (m *Matrix) handleDM(source mautrix.EventSource, ev *event.Event) {
	m.Lock()

	for userID, rooms := range *ev.Content.AsDirectChats() {
		fmt.Printf("direct chat %#v\n", rooms)
		for _, roomID := range rooms {
			if _, ok := m.channels[roomID]; !ok {
				m.channels[roomID] = &Channel{
					Members: make(map[id.UserID]*User),
				}
			}

			u := &User{
				ID:                 userID,
				MemberEventContent: &event.MemberEventContent{},
			}

			m.users[userID] = u

			m.channels[roomID].Lock()
			m.channels[roomID].IsDirect = true

			if _, ok := m.channels[roomID].Members[userID]; !ok {
				m.channels[roomID].Members[userID] = u
			}

			m.channels[roomID].Unlock()
			//m.dmChannels[room] = []id.UserID{u}
		}
	}

	m.Unlock()
}

func (m *Matrix) handleMember(source mautrix.EventSource, ev *event.Event) {
	m.Lock()

	if member, ok := ev.Content.Parsed.(*event.MemberEventContent); ok {
		if user, ok := m.users[ev.Sender]; !ok {
			m.users[ev.Sender] = &User{
				ID:                 ev.Sender,
				MemberEventContent: member,
			}
		} else if member.IsDirect {
			user.IsDirect = true
			if _, ok := m.channels[ev.RoomID]; !ok {
				m.channels[ev.RoomID] = &Channel{
					Members: make(map[id.UserID]*User),
				}
			}
			m.channels[ev.RoomID].IsDirect = true
			//m.channels[ev.RoomID].Members
		}
	}

	m.Unlock()
}

func (m *Matrix) handleRoomName(source mautrix.EventSource, ev *event.Event) {
	m.Lock()
	defer m.Unlock()

	if _, ok := m.channels[ev.RoomID]; !ok {
		m.channels[ev.RoomID] = &Channel{}
	} else {
		return
	}

	m.channels[ev.RoomID].Lock()
	m.channels[ev.RoomID].Alias = id.RoomAlias("#" + strings.ReplaceAll(ev.Content.AsRoomName().Name, " ", ""))
	m.channels[ev.RoomID].Unlock()
}

func (m *Matrix) handleCreate(source mautrix.EventSource, ev *event.Event) {
	/*
		m.Lock()
		if _,ok := m.channels[ev.RoomID];!ok {
			m.channels[ev.RoomID]=
		}
	*/
}

func (m *Matrix) handleCanonicalAlias(source mautrix.EventSource, ev *event.Event) {
	fmt.Println("running handleCanonicalAlias for", ev)
	if _, ok := m.channels[ev.RoomID]; !ok {
		m.channels[ev.RoomID] = &Channel{}
	}

	m.channels[ev.RoomID].Lock()
	m.channels[ev.RoomID].Alias = ev.Content.AsCanonicalAlias().Alias
	m.channels[ev.RoomID].AltAliases = ev.Content.AsCanonicalAlias().AltAliases
	m.channels[ev.RoomID].Unlock()

	//m.mc.JoinedMembers(ev.RoomID)
}

func (m *Matrix) handleEvent(source mautrix.EventSource, ev *event.Event) {
	text, _ := ev.Content.Raw["body"].(string)

	ghost := m.createUser(ev.Sender)

	if ghost.Me {
		return
	}

	m.RLock()
	_, ok := m.dmChannels[ev.RoomID]
	m.RUnlock()

	if ok {
		event := &bridge.Event{
			Type: "direct_message",
			Data: &bridge.DirectMessageEvent{
				Text:      text,
				ChannelID: ev.RoomID.String(),
				Sender:    ghost,
				Receiver:  m.GetMe(),
				//Files:       m.getFilesFromData(data),
				MessageID: string(ev.ID),
				//Event:       rmsg.Event,
				//ParentID:    mxEvent
			},
		}

		m.eventChan <- event
		return
	}

	event := &bridge.Event{
		Type: "channel_message",
		Data: &bridge.ChannelMessageEvent{
			Text:        text,
			ChannelID:   ev.RoomID.String(),
			Sender:      ghost,
			ChannelType: "P",
			//Files:       m.getFilesFromData(data),
			MessageID: string(ev.ID),
			//Event:       rmsg.Event,
			//ParentID:    mxEvent
		},
	}

	m.eventChan <- event
}

func (m *Matrix) Invite(channelID, username string) error {
	return nil
}

func (m *Matrix) Join(channelName string) (string, string, error) {
	resp, err := m.mc.JoinRoom(channelName, "", nil)
	if err != nil {
		return "", "", err
	}

	return resp.RoomID.String(), "", err
}

func (m *Matrix) List() (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *Matrix) Part(channelID string) error {
	//	m.mc.Client.RemoveUserFromChannel(channelID, m.mc.User.Id)

	return nil
}

func (m *Matrix) UpdateChannels() error {
	// return m.mc.UpdateChannels()
	return nil
}

func (m *Matrix) Logout() error {
	return nil
}

func (m *Matrix) MsgUser(userID, text string) (string, error) {
	return m.MsgUserThread(userID, "", text)
}

func (m *Matrix) MsgUserThread(userID, parentID, text string) (string, error) {
	fmt.Println("sending message", userID, parentID, text)
	invites := []id.UserID{id.UserID(userID)}
	req := &mautrix.ReqCreateRoom{
		Preset:   "trusted_private_chat",
		Invite:   invites,
		IsDirect: true,
	}

	resp, err := m.mc.CreateRoom(req)
	if err != nil {
		fmt.Println("msguserthread sending message: error", err)
		return "", err
	}

	fmt.Println("msguserthread sending message: error,resp", err, resp)

	m.Lock()
	m.dmChannels[id.RoomID(resp.RoomID)] = invites
	m.Unlock()

	return m.MsgChannelThread(resp.RoomID.String(), parentID, text)
}

func (m *Matrix) MsgChannel(channelID, text string) (string, error) {
	return m.MsgChannelThread(channelID, "", text)
}

func (m *Matrix) MsgChannelThread(channelID, parentID, text string) (string, error) {
	fmt.Println("msgchannelthread: sending message thread", channelID, parentID, text)
	resp, err := m.mc.SendMessageEvent(id.RoomID(channelID), event.EventMessage, event.MessageEventContent{
		MsgType:       "m.text",
		Body:          text,
		FormattedBody: helper.ParseMarkdown(text),
		Format:        "org.matrix.custom.html",
	})
	if err != nil {
		return "", err
	}

	fmt.Println("msgchannelthread: error,resp", err, resp)

	return resp.EventID.String(), nil
}

func (m *Matrix) ModifyPost(msgID, text string) error {
	return nil
}

func (m *Matrix) Topic(channelID string) string {
	return ""
	//	return m.mc.GetChannelHeader(channelID)
}

func (m *Matrix) SetTopic(channelID, text string) error {
	return nil
	/*
		logger.Debugf("updating channelheader %#v, %#v", channelID, text)
		patch := &model.ChannelPatch{
			Header: &text,
		}

		_, resp := m.mc.Client.PatchChannel(channelID, patch)
		if resp.Error != nil {
			return resp.Error
		}

		return nil
	*/
}

func (m *Matrix) StatusUser(userID string) (string, error) {
	return "", nil
	//return m.mc.GetStatus(userID), nil
}

func (m *Matrix) StatusUsers() (map[string]string, error) {
	return map[string]string{}, nil
	//	return m.mc.GetStatuses(), nil
}

func (m *Matrix) Protocol() string {
	return "matrix"
}

func (m *Matrix) Kick(channelID, username string) error {
	return nil
	/*
		_, resp := m.mc.Client.RemoveUserFromChannel(channelID, username)
		if resp.Error != nil {
			return resp.Error
		}

		return nil
	*/
}

func (m *Matrix) SetStatus(status string) error {
	return nil
	/*
		_, resp := m.mc.Client.UpdateUserStatus(m.mc.User.Id, &model.Status{
			Status: status,
			UserId: m.mc.User.Id,
		})
		if resp.Error != nil {
			return resp.Error
		}

		return nil
	*/
}

func (m *Matrix) Nick(name string) error {
	return nil
	//return m.mc.UpdateUserNick(name)
}

func (m *Matrix) GetChannelName(channelID string) string {
	for _, channel := range m.GetChannels() {
		if channel.ID == channelID {
			return channel.Name
		}
	}

	/*
		resp, err := m.mc.Members(id.RoomID(channelID))
		fmt.Println("getchannelname", err)
		spew.Dump(resp)
	*/
	/*
		resp,err := m.mc.JoinedRooms()
		resp.JoinedRooms
		var name string

		channelName := m.mc.GetChannelName(channelID)

		if channelName == "" {
			m.mc.UpdateChannels()
		}

		channelName = m.mc.GetChannelName(channelID)

		// return DM channels immediately
		if strings.Contains(channelName, "__") {
			return channelName
		}

		teamID := m.mc.GetTeamFromChannel(channelID)
		teamName := m.mc.GetTeamName(teamID)

		if channelName != "" {
			if (teamName != "" && teamID != m.mc.Team.ID) || m.v.GetBool("mattermost.PrefixMainTeam") {
				name = "#" + teamName + "/" + channelName
			}
			if teamID == m.mc.Team.ID && !m.v.GetBool("mattermost.PrefixMainTeam") {
				name = "#" + channelName
			}
			if teamID == "G" {
				name = "#" + channelName
			}
		} else {
			name = channelID
		}

		return name
	*/
	return channelID
}

func (m *Matrix) GetChannelUsers(channelID string) ([]*bridge.UserInfo, error) {

	//return m.channels[id.RoomID(channelID)].Members
	var users []*bridge.UserInfo

	resp, err := m.mc.JoinedMembers(id.RoomID(channelID))
	if err != nil {
		return nil, err
	}

	//fmt.Println("getchannelusers", channelID, len(resp.Joined))

	for user := range resp.Joined {
		users = append(users, m.createUser(user))
	}

	return users, nil
}

func (m *Matrix) GetUsers() []*bridge.UserInfo {
	var users []*bridge.UserInfo

	return users
	/*
		for _, mmuser := range m.mc.GetUsers() {
			users = append(users, m.createUser(mmuser))
		}

		return users
	*/
}

func (m *Matrix) GetChannels() []*bridge.ChannelInfo {
	var channels []*bridge.ChannelInfo

	m.RLock()
	defer m.RUnlock()

	for roomID, channel := range m.channels {
		channel.RLock()

		if channel.IsDirect && channel.Alias == "" {
			channel.Alias = id.RoomAlias(roomID.String())
		}

		channels = append(channels, &bridge.ChannelInfo{
			Name:    strings.Replace(channel.Alias.String(), ":", "/", 1),
			ID:      roomID.String(),
			DM:      channel.IsDirect,
			Private: false,
		})

		channel.RUnlock()
	}

	return channels
}

func (m *Matrix) GetChannel(channelID string) (*bridge.ChannelInfo, error) {
	for _, channel := range m.GetChannels() {
		if channel.ID == channelID {
			return channel, nil
		}
	}

	return nil, errors.New("channel not found")
}

func (m *Matrix) GetUser(userID string) *bridge.UserInfo {
	return m.createUser(id.UserID(userID))
}

func (m *Matrix) GetMe() *bridge.UserInfo {
	return m.createUser(m.mc.UserID)
}

func (m *Matrix) GetUserByUsername(username string) *bridge.UserInfo {
	/*
		for {
			mmuser, resp := m.mc.Client.GetUserByUsername(username, "")
			if resp.Error == nil {
				return m.createUser(mmuser)
			}

			if err := m.mc.HandleRatelimit("GetUserByUsername", resp); err != nil {
				return &bridge.UserInfo{}
			}
		}
	*/
	return nil
}

func (m *Matrix) createUser(userID id.UserID) *bridge.UserInfo {
	var me bool

	if userID == m.mc.UserID {
		me = true
	}

	nick, host, err := userID.Parse()
	if err != nil {
		return nil
	}

	displayName := nick + "@" + host

	m.RLock()

	if user, ok := m.users[userID]; ok {
		displayName = user.Displayname
	}

	m.RUnlock()

	info := &bridge.UserInfo{
		Nick: nick + "@" + host,
		User: userID.String(),
		Real: displayName,
		Host: host,
		//Roles:       mmuser.Roles,
		Ghost: true,
		Me:    me,
		//TeamID:      teamID,
		Username: nick,
		//FirstName:   mmuser.FirstName,
		//LastName:    mmuser.LastName,
		//MentionKeys: strings.Split(mentionkeys, ","),
	}

	return info
}

func isValidNick(s string) bool {
	/* IRC RFC ([0] - see below) mentions a limit of 9 chars for
	 * IRC nicks, but modern clients allow more than that. Let's
	 * use a "sane" big value, the triple of the spec.
	 */
	if len(s) < 1 || len(s) > 27 {
		return false
	}

	/* According to IRC RFC [0], the allowed chars to have as nick
	 * are: ( letter / special-'-' ).*( letter / digit / special ),
	 * where:
	 * letter = [a-z / A-Z]; digit = [0-9];
	 * special = [';', '[', '\', ']', '^', '_', '`', '{', '|', '}', '-']
	 *
	 * ASCII codes (decimal) for the allowed chars:
	 * letter = [65-90,97-122]; digit = [48-57]
	 * special = [59, 91-96, 123-125, 45]
	 * [0] RFC 2812 (tools.ietf.org/html/rfc2812)
	 */

	if s[0] != 59 && (s[0] < 65 || s[0] > 125) {
		return false
	}

	for i := 1; i < len(s); i++ {
		if s[i] != 45 && s[i] != 59 && (s[i] < 65 || s[i] > 125) {
			if s[i] < 48 || s[i] > 57 {
				return false
			}
		}
	}

	return true
}

// maybeShorten returns a prefix of msg that is approximately newLen
// characters long, followed by "...".  Words that start with uncounted
// are included in the result but are not reckoned against newLen.
func maybeShorten(msg string, newLen int, uncounted string, unicode bool) string {
	if newLen == 0 || len(msg) < newLen {
		return msg
	}
	ellipsis := "..."
	if unicode {
		ellipsis = "…"
	}
	newMsg := ""
	for _, word := range strings.Split(strings.ReplaceAll(msg, "\n", " "), " ") {
		if newMsg == "" {
			newMsg = word
			continue
		}
		if len(newMsg) < newLen {
			skipped := false
			if uncounted != "" && strings.HasPrefix(word, uncounted) {
				newLen += len(word) + 1
				skipped = true
			}
			// Truncate very long words, but only if they were not skipped, on the
			// assumption that such words are important enough to be preserved whole.
			if !skipped && len(word) > newLen {
				word = fmt.Sprintf("%s[%s]", word[0:(newLen*2/3)], ellipsis)
			}
			newMsg = fmt.Sprintf("%s %s", newMsg, word)
			continue
		}
		break
	}

	return fmt.Sprintf("%s %s", newMsg, ellipsis)
}

func (m *Matrix) GetTeamName(teamID string) string {
	return ""
	//	return m.mc.GetTeamName(teamID)
}

func (m *Matrix) GetLastViewedAt(channelID string) int64 {
	return 0
	/*
		x := m.mc.GetLastViewedAt(channelID)
		logger.Tracef("getLastViewedAt %s: %#v", channelID, x)

		return x
	*/
}

func (m *Matrix) GetPostsSince(channelID string, since int64) interface{} {
	return nil
	//	return m.mc.GetPostsSince(channelID, since)
}

func (m *Matrix) UpdateLastViewed(channelID string) {
	return

}

func (m *Matrix) UpdateLastViewedUser(userID string) error {
	return nil
}

func (m *Matrix) SearchPosts(search string) interface{} {
	return nil
}

func (m *Matrix) GetFileLinks(fileIDs []string) []string {
	return []string{}
}

func (m *Matrix) SearchUsers(query string) ([]*bridge.UserInfo, error) {
	var brusers []*bridge.UserInfo
	return brusers, nil
}

func (m *Matrix) GetPosts(channelID string, limit int) interface{} {
	return nil
	//	return m.mc.GetPosts(channelID, limit)
}

func (m *Matrix) GetChannelID(name, teamID string) string {
	for _, channel := range m.GetChannels() {
		if channel.Name == name {
			return channel.ID
		}
	}

	return ""
	//	return m.mc.GetChannelID(name, teamID)
}

func (m *Matrix) Connected() bool {
	return m.connected
}