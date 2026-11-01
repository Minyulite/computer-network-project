package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	db      *store.DB
	chat    *Service
	a, b, c auth.Identity
}

func setup(t *testing.T) fixture {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required for T4 integration checks")
	}
	if raw == os.Getenv("DATABASE_URL") {
		t.Fatal("test URL must name a dedicated database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatal("test database unavailable")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "t4_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String(), 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := store.Migrate(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	authService, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	makeIdentity := func(name, ip string) auth.Identity {
		result, err := authService.Signup(ctx, name, "fifteen characters", ip, "")
		if err != nil {
			t.Fatal(err)
		}
		identity, err := authService.ValidateSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	return fixture{db: db, chat: New(db),
		a: makeIdentity("alice", "192.0.2.1"), b: makeIdentity("bob", "192.0.2.2"),
		c: makeIdentity("charlie", "192.0.2.3")}
}

func mustUUID(t *testing.T) string {
	t.Helper()
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func TestConcurrentDirectOpenAndPrivacy(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	type opened struct {
		room    Room
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan opened, 2)
	go func() {
		<-start
		room, created, err := f.chat.OpenDirect(ctx, f.a, f.b.User.ID)
		results <- opened{room, created, err}
	}()
	go func() {
		<-start
		room, created, err := f.chat.OpenDirect(ctx, f.b, f.a.User.ID)
		results <- opened{room, created, err}
	}()
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.room.ID == "" || first.room.ID != second.room.ID || first.created == second.created {
		t.Fatalf("concurrent opens: %+v / %+v", first, second)
	}
	room := first.room
	if room.Kind != "direct" || room.HistoryStartSeq != 1 || room.DirectUserLow == room.DirectUserHigh {
		t.Fatalf("invalid direct room: %+v", room)
	}
	var roomCount, memberCount int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM rooms WHERE kind='direct'").Scan(&roomCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM room_members WHERE room_id=$1", room.ID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if roomCount != 1 || memberCount != 2 {
		t.Fatalf("rooms=%d members=%d", roomCount, memberCount)
	}
	again, created, err := f.chat.OpenDirect(ctx, f.a, f.b.User.ID)
	if err != nil || created || again.ID != room.ID {
		t.Fatalf("offline reopen: %+v %v %v", again, created, err)
	}
	if _, _, err := f.chat.OpenDirect(ctx, f.a, f.a.User.ID); !errors.Is(err, ErrSelfChat) {
		t.Fatalf("self chat: %v", err)
	}
	if _, _, err := f.chat.OpenDirect(ctx, f.a, mustUUID(t)); !errors.Is(err, ErrUserUnavailable) {
		t.Fatalf("missing account: %v", err)
	}
	groups, err := f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil || len(groups.Items) != 0 {
		t.Fatalf("C group catalogue: %+v %v", groups, err)
	}
	rooms, err := f.chat.OwnRooms(ctx, f.c, PageOptions{})
	if err != nil || len(rooms.Items) != 0 {
		t.Fatalf("C room list: %+v %v", rooms, err)
	}
	for _, member := range []auth.Identity{f.a, f.b} {
		own, err := f.chat.OwnRooms(ctx, member, PageOptions{})
		if err != nil || len(own.Items) != 1 || own.Items[0].ID != room.ID {
			t.Fatalf("participant room list: %+v %v", own, err)
		}
	}
	if _, err := f.chat.GroupMembers(ctx, f.c, room.ID, PageOptions{}); !errors.Is(err, ErrNotGroup) {
		t.Fatalf("direct members exposed as group: %v", err)
	}
	if _, err := f.chat.History(ctx, f.c, room.ID, HistoryOptions{}); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("C history: %v", err)
	}
	if _, err := f.chat.History(ctx, f.c, mustUUID(t), HistoryOptions{}); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("missing and inaccessible rooms differ: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.c, room.ID, mustUUID(t), "secret"); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("C send: %v", err)
	}
	sent, err := f.chat.SendMessage(ctx, f.a, room.ID, mustUUID(t), "  secret  ")
	if err != nil || !sent.Created || sent.Message.Text != "secret" || len(sent.RecipientUserIDs) != 2 ||
		!containsID(sent.RecipientUserIDs, f.a.User.ID) || !containsID(sent.RecipientUserIDs, f.b.User.ID) || containsID(sent.RecipientUserIDs, f.c.User.ID) {
		t.Fatalf("direct send: %+v %v", sent, err)
	}
	bHistory, err := f.chat.History(ctx, f.b, room.ID, HistoryOptions{})
	if err != nil || len(bHistory.Messages) != 1 || bHistory.Messages[0].Sender.Username != "alice" {
		t.Fatalf("B history: %+v %v", bHistory, err)
	}
}

func TestGroupDiscoveryJoinAndHistoryBoundary(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	group, err := f.chat.CreateGroup(ctx, f.a, "  Team 💬  ")
	if err != nil || group.Name != "Team 💬" || group.HistoryStartSeq != 1 {
		t.Fatalf("create group: %+v %v", group, err)
	}
	members, err := f.chat.GroupMembers(ctx, f.c, group.ID, PageOptions{})
	if err != nil || len(members.Items) != 1 || members.Items[0].User.ID != f.a.User.ID {
		t.Fatalf("creator-only members: %+v %v", members, err)
	}
	catalogue, err := f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil || len(catalogue.Items) != 1 || catalogue.Items[0].ID != group.ID {
		t.Fatalf("public catalogue: %+v %v", catalogue, err)
	}
	if _, err := f.chat.History(ctx, f.b, group.ID, HistoryOptions{}); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("nonmember history: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.b, group.ID, mustUUID(t), "no access"); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("nonmember send: %v", err)
	}
	pre, err := f.chat.SendMessage(ctx, f.a, group.ID, mustUUID(t), "before joining")
	if err != nil || pre.Message.Seq != 1 || len(pre.RecipientUserIDs) != 1 {
		t.Fatalf("pre-join send: %+v %v", pre, err)
	}
	joined, err := f.chat.JoinGroup(ctx, f.b, group.ID)
	if err != nil || !joined.Created || joined.Room.HistoryStartSeq != 2 {
		t.Fatalf("join boundary: %+v %v", joined, err)
	}
	repeat, err := f.chat.JoinGroup(ctx, f.b, group.ID)
	if err != nil || repeat.Created || repeat.Room.HistoryStartSeq != 2 {
		t.Fatalf("repeat join: %+v %v", repeat, err)
	}
	bHistory, err := f.chat.History(ctx, f.b, group.ID, HistoryOptions{})
	if err != nil || len(bHistory.Messages) != 0 || bHistory.HistoryStartSeq != 2 {
		t.Fatalf("pre-join history leaked: %+v %v", bHistory, err)
	}
	post, err := f.chat.SendMessage(ctx, f.b, group.ID, mustUUID(t), "after joining")
	if err != nil || post.Message.Seq != 2 || len(post.RecipientUserIDs) != 2 || containsID(post.RecipientUserIDs, f.c.User.ID) {
		t.Fatalf("post-join recipients: %+v %v", post, err)
	}
	bHistory, err = f.chat.History(ctx, f.b, group.ID, HistoryOptions{})
	if err != nil || len(bHistory.Messages) != 1 || bHistory.Messages[0].Seq != 2 {
		t.Fatalf("B history after join: %+v %v", bHistory, err)
	}
	members, err = f.chat.GroupMembers(ctx, f.c, group.ID, PageOptions{Limit: 1})
	if err != nil || len(members.Items) != 1 || members.NextCursor == "" {
		t.Fatalf("member page: %+v %v", members, err)
	}
	nextMembers, err := f.chat.GroupMembers(ctx, f.c, group.ID, PageOptions{Limit: 1, Cursor: members.NextCursor})
	if err != nil || len(nextMembers.Items) != 1 || nextMembers.Items[0].User.ID == members.Items[0].User.ID {
		t.Fatalf("next member page: %+v %v", nextMembers, err)
	}
	catalogue, err = f.chat.Groups(ctx, f.c, PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	publicJSON, err := json.Marshal(catalogue)
	if err != nil || strings.Contains(string(publicJSON), "before joining") || strings.Contains(string(publicJSON), pre.Message.ID) || strings.Contains(string(publicJSON), "directUser") {
		t.Fatal("group discovery leaked message or direct metadata")
	}
	cRooms, err := f.chat.OwnRooms(ctx, f.c, PageOptions{})
	if err != nil || len(cRooms.Items) != 0 {
		t.Fatalf("nonmember own rooms: %+v %v", cRooms, err)
	}
	second, err := f.chat.CreateGroup(ctx, f.a, "Second")
	if err != nil {
		t.Fatal(err)
	}
	page1, err := f.chat.Groups(ctx, f.c, PageOptions{Limit: 1})
	if err != nil || len(page1.Items) != 1 || page1.NextCursor == "" {
		t.Fatalf("group page 1: %+v %v", page1, err)
	}
	page2, err := f.chat.Groups(ctx, f.c, PageOptions{Limit: 1, Cursor: page1.NextCursor})
	if err != nil || len(page2.Items) != 1 || page2.Items[0].ID == page1.Items[0].ID {
		t.Fatalf("group page 2: %+v %v", page2, err)
	}
	aRooms, err := f.chat.OwnRooms(ctx, f.a, PageOptions{Limit: 1})
	if err != nil || len(aRooms.Items) != 1 || aRooms.NextCursor == "" {
		t.Fatalf("own room page: %+v %v", aRooms, err)
	}
	aRoomsNext, err := f.chat.OwnRooms(ctx, f.a, PageOptions{Limit: 1, Cursor: aRooms.NextCursor})
	if err != nil || len(aRoomsNext.Items) != 1 || aRoomsNext.Items[0].ID == aRooms.Items[0].ID {
		t.Fatalf("own room next page: %+v %v", aRoomsNext, err)
	}
	_, err = f.chat.JoinGroup(ctx, f.b, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.chat.CreateGroup(ctx, f.a, "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank group name: %v", err)
	}
	if _, err := f.chat.CreateGroup(ctx, f.a, "\u2003\u00a0"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Unicode-blank group name: %v", err)
	}
	if _, err := f.chat.CreateGroup(ctx, f.a, strings.Repeat("💬", 65)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long group name: %v", err)
	}
	if _, err := f.chat.JoinGroup(ctx, f.b, mustUUID(t)); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("missing group join: %v", err)
	}
	if _, err := f.chat.Groups(ctx, f.a, PageOptions{Limit: 51}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unbounded catalogue page: %v", err)
	}
}

func TestRetriesPaginationAndRollback(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	direct, _, err := f.chat.OpenDirect(ctx, f.a, f.b.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := f.chat.CreateGroup(ctx, f.a, "Other room")
	if err != nil {
		t.Fatal(err)
	}
	type sentResult struct {
		result SendResult
		err    error
	}
	results := make(chan sentResult, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			caller := f.a
			if i%2 == 1 {
				caller = f.b
			}
			id, err := newUUID()
			if err != nil {
				results <- sentResult{err: err}
				return
			}
			result, err := f.chat.SendMessage(ctx, caller, direct.ID, id, fmt.Sprintf("msg-%d", i))
			results <- sentResult{result, err}
		}(i)
	}
	wg.Wait()
	close(results)
	seqs := make([]int, 0, 12)
	var aMessage SendResult
	for item := range results {
		if item.err != nil || !item.result.Created {
			t.Fatalf("concurrent send: %+v", item)
		}
		seqs = append(seqs, int(item.result.Message.Seq))
		if item.result.Message.Sender.ID == f.a.User.ID {
			aMessage = item.result
		}
	}
	sort.Ints(seqs)
	for i, seq := range seqs {
		if seq != i+1 {
			t.Fatalf("sequence gap: %v", seqs)
		}
	}
	retry, err := f.chat.SendMessage(ctx, f.a, direct.ID, aMessage.Message.ClientMessageID, "  "+aMessage.Message.Text+"  ")
	if err != nil || retry.Created || retry.Message.ID != aMessage.Message.ID || len(retry.RecipientUserIDs) != 0 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, direct.ID, aMessage.Message.ClientMessageID, "changed"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed retry: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, group.ID, aMessage.Message.ClientMessageID, aMessage.Message.Text); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("cross-room retry: %v", err)
	}
	var directSeq, groupSeq int64
	if err := f.db.Pool.QueryRow(ctx, "SELECT last_seq FROM rooms WHERE id=$1", direct.ID).Scan(&directSeq); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool.QueryRow(ctx, "SELECT last_seq FROM rooms WHERE id=$1", group.ID).Scan(&groupSeq); err != nil {
		t.Fatal(err)
	}
	if directSeq != 12 || groupSeq != 0 {
		t.Fatalf("retry changed counters: direct=%d group=%d", directSeq, groupSeq)
	}
	sharedID := mustUUID(t)
	barrier := make(chan struct{})
	race := make(chan sentResult, 2)
	go func() {
		<-barrier
		result, err := f.chat.SendMessage(ctx, f.a, direct.ID, sharedID, "shared")
		race <- sentResult{result, err}
	}()
	go func() {
		<-barrier
		result, err := f.chat.SendMessage(ctx, f.a, group.ID, sharedID, "shared")
		race <- sentResult{result, err}
	}()
	close(barrier)
	one, two := <-race, <-race
	if (one.err == nil) == (two.err == nil) || (one.err != nil && !errors.Is(one.err, ErrIdempotencyConflict)) ||
		(two.err != nil && !errors.Is(two.err, ErrIdempotencyConflict)) {
		t.Fatalf("cross-room duplicate race: %+v / %+v", one, two)
	}
	var afterDirect, afterGroup int64
	if err := f.db.Pool.QueryRow(ctx, "SELECT last_seq FROM rooms WHERE id=$1", direct.ID).Scan(&afterDirect); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool.QueryRow(ctx, "SELECT last_seq FROM rooms WHERE id=$1", group.ID).Scan(&afterGroup); err != nil {
		t.Fatal(err)
	}
	if afterDirect+afterGroup != directSeq+groupSeq+1 {
		t.Fatalf("loser counter committed: %d %d", afterDirect, afterGroup)
	}
	duplicateServerID := aMessage.Message.ID
	rollbackClientID := mustUUID(t)
	var beforeCount int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE room_id=$1", direct.ID).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.db.SaveMessage(ctx, store.MessageRecord{ID: duplicateServerID, RoomID: direct.ID,
		SenderID: f.a.User.ID, ClientMessageID: rollbackClientID, Text: "should roll back"})
	if err == nil {
		t.Fatal("duplicate server message ID succeeded")
	}
	var afterRollback int64
	if err := f.db.Pool.QueryRow(ctx, "SELECT last_seq FROM rooms WHERE id=$1", direct.ID).Scan(&afterRollback); err != nil {
		t.Fatal(err)
	}
	if afterRollback != afterDirect {
		t.Fatalf("failed insert advanced counter: %d -> %d", afterDirect, afterRollback)
	}
	var afterCount int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE room_id=$1", direct.ID).Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount {
		t.Fatalf("failed insert changed message count: %d -> %d", beforeCount, afterCount)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), " \t "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank text: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), "\u2003\u00a0"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Unicode-blank text: %v", err)
	}
	if _, err := f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), strings.Repeat("💬", 2001)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long text: %v", err)
	}
	validLong, err := f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), strings.Repeat("💬", 2000))
	if err != nil || validLong.Message.Seq != afterRollback+1 {
		t.Fatalf("next committed sequence: %+v %v", validLong, err)
	}
	latest, err := f.chat.History(ctx, f.a, direct.ID, HistoryOptions{Limit: 5})
	if err != nil || len(latest.Messages) != 5 || !latest.HasMore || latest.NextBeforeSeq == nil {
		t.Fatalf("latest page: %+v %v", latest, err)
	}
	for i := 1; i < len(latest.Messages); i++ {
		if latest.Messages[i].Seq <= latest.Messages[i-1].Seq {
			t.Fatal("history not ascending")
		}
	}
	older, err := f.chat.History(ctx, f.a, direct.ID, HistoryOptions{Limit: 5, BeforeSeq: latest.NextBeforeSeq, UpToSeq: &latest.UpToSeq})
	if err != nil || len(older.Messages) != 5 || older.Messages[len(older.Messages)-1].Seq >= latest.Messages[0].Seq {
		t.Fatalf("older page: %+v %v", older, err)
	}
	zero := int64(0)
	recovery, err := f.chat.History(ctx, f.b, direct.ID, HistoryOptions{Limit: 4, AfterSeq: &zero})
	if err != nil || len(recovery.Messages) != 4 || recovery.Messages[0].Seq != 1 || recovery.NextAfterSeq == nil || !recovery.HasMore {
		t.Fatalf("recovery page: %+v %v", recovery, err)
	}
	watermark := recovery.UpToSeq
	_, err = f.chat.SendMessage(ctx, f.a, direct.ID, mustUUID(t), "new after watermark")
	if err != nil {
		t.Fatal(err)
	}
	more, err := f.chat.History(ctx, f.b, direct.ID, HistoryOptions{Limit: 100, AfterSeq: recovery.NextAfterSeq, UpToSeq: &watermark})
	if err != nil || more.UpToSeq != watermark || len(more.Messages) == 0 || more.Messages[len(more.Messages)-1].Seq != watermark {
		t.Fatalf("fixed high-water recovery: %+v %v", more, err)
	}
	if _, err := f.chat.History(ctx, f.a, direct.ID, HistoryOptions{BeforeSeq: latest.NextBeforeSeq, AfterSeq: &zero}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("two cursors: %v", err)
	}
}

func TestConcurrentJoinSendBoundary(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		room, err := f.chat.CreateGroup(ctx, f.a, fmt.Sprintf("Concurrent %d", i))
		if err != nil {
			t.Fatal(err)
		}
		clientID := mustUUID(t)
		start := make(chan struct{})
		type sendOutcome struct {
			result SendResult
			err    error
		}
		type joinOutcome struct {
			result JoinResult
			err    error
		}
		sent := make(chan sendOutcome, 1)
		joined := make(chan joinOutcome, 1)
		go func() {
			<-start
			result, err := f.chat.SendMessage(ctx, f.a, room.ID, clientID, "racing send")
			sent <- sendOutcome{result, err}
		}()
		go func() {
			<-start
			result, err := f.chat.JoinGroup(ctx, f.b, room.ID)
			joined <- joinOutcome{result, err}
		}()
		close(start)
		s, j := <-sent, <-joined
		if s.err != nil || j.err != nil || s.result.Message.Seq != 1 || !j.result.Created {
			t.Fatalf("race %d: send=%+v join=%+v", i, s, j)
		}
		bHistory, err := f.chat.History(ctx, f.b, room.ID, HistoryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		switch j.result.Room.HistoryStartSeq {
		case 1:
			if len(bHistory.Messages) != 1 || !containsID(s.result.RecipientUserIDs, f.b.User.ID) {
				t.Fatalf("join-before-send boundary %d: %+v %+v", i, bHistory, s)
			}
		case 2:
			if len(bHistory.Messages) != 0 || containsID(s.result.RecipientUserIDs, f.b.User.ID) {
				t.Fatalf("send-before-join boundary %d: %+v %+v", i, bHistory, s)
			}
		default:
			t.Fatalf("unexpected boundary %d", j.result.Room.HistoryStartSeq)
		}
	}
}
