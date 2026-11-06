package realtime

// Frame types the server sends. Command types are the keys of routes.
const (
	frameOK    = "request.ok"
	frameError = "request.error"

	eventSessionReady      = "session.ready"
	eventPresenceChanged   = "presence.changed"
	eventRoomUpserted      = "room.upserted"
	eventGroupCreated      = "group.created"
	eventGroupMemberJoined = "group.member_joined"
	eventMessageCreated    = "message.created"
	eventBotUpserted       = "bot.upserted"
)
