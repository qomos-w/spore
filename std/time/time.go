package time

import (
	"time"

	"github.com/qomos-w/spore/binding"
)

// TimeInfo exposes multiple time representations for script consumption.
type TimeInfo struct {
	Unix      int64  `json:"unix"`
	UnixMilli int64  `json:"unixMilli"`
	UnixNano  int64  `json:"unixNano"`
	RFC3339   string `json:"rfc3339"`
}

// Register registers the time standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("time", "module")

	if err := builder.AddFreeFunction("nowUnix", func() int64 { return time.Now().Unix() }); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("now", func() (TimeInfo, error) {
		t := time.Now()
		return TimeInfo{
			Unix:      t.Unix(),
			UnixMilli: t.UnixMilli(),
			UnixNano:  t.UnixNano(),
			RFC3339:   t.Format(time.RFC3339),
		}, nil
	}); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("format", func(unix int64, layout string) string {
		return time.Unix(unix, 0).Format(layout)
	}); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("time")
}
