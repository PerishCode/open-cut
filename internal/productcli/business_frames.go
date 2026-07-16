package productcli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/command"
	"github.com/PerishCode/open-cut/product/domain"
)

func parseAssetFramesInvocation(
	set *flag.FlagSet,
	args []string,
) (domain.AssetID, domain.SourceStreamID, command.AssetFramesInput, error) {
	asset := set.String("asset-id", "", "Asset identity")
	stream := set.String("source-stream-id", "", "exact video SourceStream identity")
	var timeArguments stringListFlag
	set.Var(&timeArguments, "time", "exact source time as value/scale seconds; repeat one to eight times")
	if err := set.Parse(args); err != nil || set.NArg() != 0 ||
		len(timeArguments) == 0 || len(timeArguments) > application.MaximumFrameSetSamples {
		return domain.AssetID{}, domain.SourceStreamID{}, command.AssetFramesInput{},
			fmt.Errorf("invalid asset frames invocation")
	}
	assetID, err := domain.ParseAssetID(*asset)
	if err != nil {
		return domain.AssetID{}, domain.SourceStreamID{}, command.AssetFramesInput{},
			fmt.Errorf("invalid Asset identity")
	}
	streamID, err := domain.ParseSourceStreamID(*stream)
	if err != nil {
		return domain.AssetID{}, domain.SourceStreamID{}, command.AssetFramesInput{},
			fmt.Errorf("invalid SourceStream identity")
	}
	times := make([]domain.RationalTime, 0, len(timeArguments))
	for _, value := range timeArguments {
		instant, parseErr := parseRationalArgument(value, false)
		if parseErr != nil || instant.IsNegative() {
			return domain.AssetID{}, domain.SourceStreamID{}, command.AssetFramesInput{},
				fmt.Errorf("invalid frame source time")
		}
		if len(times) > 0 {
			comparison, compareErr := times[len(times)-1].Compare(instant)
			if compareErr != nil || comparison >= 0 {
				return domain.AssetID{}, domain.SourceStreamID{}, command.AssetFramesInput{},
					fmt.Errorf("frame source times must be strictly increasing")
			}
		}
		times = append(times, instant)
	}
	return assetID, streamID, command.AssetFramesInput{
		AssetID: assetID, SourceStreamID: streamID, Times: times,
	}, nil
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	if values == nil {
		return ""
	}
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("flag value is empty")
	}
	*values = append(*values, value)
	return nil
}
