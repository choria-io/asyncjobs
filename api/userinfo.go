package api

import (
	"fmt"

	aj "github.com/choria-io/asyncjobs"
	"github.com/dustin/go-humanize"
)

type QueueUserInfo struct {
	name       string
	entryNum   string
	entryBytes string
}

func MakeQueueInfo(q *aj.QueueInfo) string {
	var i QueueUserInfo

	i.name = q.Name
	i.entryNum = humanize.Comma(int64(q.Stream.State.Msgs))
	i.entryBytes = humanize.IBytes(q.Stream.State.Bytes)

	return fmt.Sprintf(`
	Queue Info:
	Name: %q
	Entries: %q
	Total Bytes: %q
	`, i.name, i.entryNum, i.entryBytes)
}
