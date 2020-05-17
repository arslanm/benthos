package processor

import (
	"time"

	"github.com/Jeffail/benthos/v3/lib/bloblang/x/mapping"
	"github.com/Jeffail/benthos/v3/lib/log"
	"github.com/Jeffail/benthos/v3/lib/message"
	"github.com/Jeffail/benthos/v3/lib/message/tracing"
	"github.com/Jeffail/benthos/v3/lib/metrics"
	"github.com/Jeffail/benthos/v3/lib/types"
	"github.com/opentracing/opentracing-go"
	olog "github.com/opentracing/opentracing-go/log"
	"golang.org/x/xerrors"
)

//------------------------------------------------------------------------------

func init() {
	Constructors[TypeBloblang] = TypeSpec{
		constructor: NewBloblang,
		Summary: `
BETA: This a beta component and therefore subject to change outside of major
version releases. Consult the changelog for changes before upgrading.

Executes a [Bloblang](/docs/guides/bloblang/about) mapping on messages.`,
		Description: `
For more information about Bloblang
[check out the docs](/docs/guides/bloblang/about).`,
		Footnotes: `
## Examples

Given JSON documents containing an array of fans:

` + "```json" + `
{
  "id":"foo",
  "description":"a show about foo",
  "fans":[
    {"name":"bev","obsession":0.57},
    {"name":"grace","obsession":0.21},
    {"name":"ali","obsession":0.89},
    {"name":"vic","obsession":0.43}
  ]
}
` + "```" + `

We can reduce the fans to only those with an obsession score above 0.5 with this
mapping:

` + "```yaml" + `
pipeline:
  processors:
  - bloblang: |
      root = this
      fans = fans.map_each(match {
        this.obsession > 0.5 => this
        _ => deleted()
      })
` + "```" + `

Giving us:

` + "```json" + `
{
  "id":"foo",
  "description":"a show about foo",
  "fans":[
    {"name":"bev","obsession":0.57},
    {"name":"ali","obsession":0.89}
  ]
}
` + "```" + ``,
	}
}

//------------------------------------------------------------------------------

// BloblangConfig contains configuration fields for the Bloblang processor.
type BloblangConfig string

// NewBloblangConfig returns a BloblangConfig with default values.
func NewBloblangConfig() BloblangConfig {
	return ""
}

//------------------------------------------------------------------------------

// Bloblang is a processor that performs a Bloblang mapping.
type Bloblang struct {
	exec *mapping.Executor

	log   log.Modular
	stats metrics.Type

	mCount     metrics.StatCounter
	mErr       metrics.StatCounter
	mSent      metrics.StatCounter
	mBatchSent metrics.StatCounter
}

// NewBloblang returns a Bloblang processor.
func NewBloblang(
	conf Config, mgr types.Manager, log log.Modular, stats metrics.Type,
) (Type, error) {
	exec, err := mapping.NewExecutor(string(conf.Bloblang))
	if err != nil {
		return nil, xerrors.Errorf("failed to parse mapping: %w", err)
	}

	return &Bloblang{
		exec: exec,

		log:   log,
		stats: stats,

		mCount:     stats.GetCounter("count"),
		mErr:       stats.GetCounter("error"),
		mSent:      stats.GetCounter("sent"),
		mBatchSent: stats.GetCounter("batch.sent"),
	}, nil
}

//------------------------------------------------------------------------------

// ProcessMessage applies the processor to a message, either creating >0
// resulting messages or a response to be sent back to the message source.
func (b *Bloblang) ProcessMessage(msg types.Message) ([]types.Message, types.Response) {
	b.mCount.Incr(1)

	newParts := make([]types.Part, 0, msg.Len())

	msg.Iter(func(i int, part types.Part) error {
		span := tracing.GetSpan(part)
		if span == nil {
			span = opentracing.StartSpan(TypeBloblang)
		} else {
			span = opentracing.StartSpan(
				TypeBloblang,
				opentracing.ChildOf(span.Context()),
			)
		}

		p, err := b.exec.MapPart(i, msg)
		if err != nil {
			p = part.Copy()
			b.mErr.Incr(1)
			b.log.Errorf("%v\n", err)
			FlagErr(p, err)
			span.SetTag("error", true)
			span.LogFields(
				olog.String("event", "error"),
				olog.String("type", err.Error()),
			)
		}

		span.Finish()
		if err != nil || p != nil {
			newParts = append(newParts, p)
		}
		return nil
	})

	newMsg := message.New(nil)
	newMsg.SetAll(newParts)

	b.mBatchSent.Incr(1)
	b.mSent.Incr(int64(newMsg.Len()))
	return []types.Message{newMsg}, nil
}

// CloseAsync shuts down the processor and stops processing requests.
func (b *Bloblang) CloseAsync() {
}

// WaitForClose blocks until the processor has closed down.
func (b *Bloblang) WaitForClose(timeout time.Duration) error {
	return nil
}

//------------------------------------------------------------------------------