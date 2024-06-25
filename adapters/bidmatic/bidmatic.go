package bidmatic

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v2/adapters"
	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/errortypes"
	"github.com/prebid/prebid-server/v2/openrtb_ext"
)

type Adapter struct {
	endpoint string
}

type bidmaticImpExt struct {
	Bidmatic openrtb_ext.ExtImpBidmatic `json:"bidmatic"`
}

func (a *Adapter) MakeRequests(request *openrtb2.BidRequest, reqInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {

	totalImps := len(request.Imp)
	errors := make([]error, 0, totalImps)
	imp2source := make(map[int][]int)

	for i := 0; i < totalImps; i++ {

		sourceId, err := validateImpression(&request.Imp[i])

		if err != nil {
			errors = append(errors, err)
			continue
		}

		if _, ok := imp2source[sourceId]; !ok {
			imp2source[sourceId] = make([]int, 0, totalImps-i)
		}

		imp2source[sourceId] = append(imp2source[sourceId], i)

	}

	totalReqs := len(imp2source)
	if 0 == totalReqs {
		return nil, errors
	}

	headers := http.Header{}
	headers.Add("Content-Type", "application/json;charset=utf-8")
	headers.Add("Accept", "application/json")

	reqs := make([]*adapters.RequestData, 0, totalReqs)

	imps := request.Imp
	request.Imp = make([]openrtb2.Imp, 0, len(imps))
	for sourceId, impIds := range imp2source {
		request.Imp = request.Imp[:0]

		for i := 0; i < len(impIds); i++ {
			request.Imp = append(request.Imp, imps[impIds[i]])
		}

		body, err := json.Marshal(request)
		if err != nil {
			errors = append(errors, fmt.Errorf("error while encoding bidRequest, err: %s", err))
			return nil, errors
		}

		reqs = append(reqs, &adapters.RequestData{
			Method:  "POST",
			Uri:     a.endpoint + fmt.Sprintf("?source=%d", sourceId),
			Body:    body,
			Headers: headers,
			ImpIDs:  openrtb_ext.GetImpIDs(request.Imp),
		})
	}

	if 0 == len(reqs) {
		return nil, errors
	}

	return reqs, errors

}

func (a *Adapter) MakeBids(bidReq *openrtb2.BidRequest, unused *adapters.RequestData, httpRes *adapters.ResponseData) (*adapters.BidderResponse, []error) {

	if httpRes.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var bidResp openrtb2.BidResponse
	if err := json.Unmarshal(httpRes.Body, &bidResp); err != nil {
		return nil, []error{&errortypes.BadServerResponse{
			Message: fmt.Sprintf("error while decoding response,  err: %s", err),
		}}
	}

	bidResponse := adapters.NewBidderResponse()
	var errors []error

	var impOK bool
	for _, sb := range bidResp.SeatBid {
		for i := 0; i < len(sb.Bid); i++ {

			bid := sb.Bid[i]

			impOK = false
			mediaType := openrtb_ext.BidTypeBanner
			for _, imp := range bidReq.Imp {
				if imp.ID == bid.ImpID {

					impOK = true

					if imp.Video != nil {
						mediaType = openrtb_ext.BidTypeVideo
						break
					}
				}
			}

			if !impOK {
				errors = append(errors, &errortypes.BadServerResponse{
					Message: fmt.Sprintf("ignoring bid id=%s, request doesn't contain any impression with id=%s", bid.ID, bid.ImpID),
				})
				continue
			}

			bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
				Bid:     &bid,
				BidType: mediaType,
			})
		}
	}

	return bidResponse, errors
}

func validateImpression(imp *openrtb2.Imp) (int, error) {

	if imp.Banner == nil && imp.Video == nil {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, bidmatic supports only Video and Banner", imp.ID),
		}
	}

	if 0 == len(imp.Ext) {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, extImpBidder is empty", imp.ID),
		}
	}

	var bidderExt adapters.ExtImpBidder

	if err := json.Unmarshal(imp.Ext, &bidderExt); err != nil {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, error while decoding extImpBidder, err: %s", imp.ID, err),
		}
	}

	impExt := openrtb_ext.ExtImpBidmatic{}
	err := json.Unmarshal(bidderExt.Bidder, &impExt)
	if err != nil {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, error while decoding impExt, err: %s", imp.ID, err),
		}
	}

	// common extension for all impressions
	var impExtBuffer []byte

	impExtBuffer, err = json.Marshal(&bidmaticImpExt{
		Bidmatic: impExt,
	})
	if err != nil {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, error while marshaling impExt, err: %s", imp.ID, err),
		}
	}

	if impExt.BidFloor > 0 {
		imp.BidFloor = impExt.BidFloor
	}

	imp.Ext = impExtBuffer

	source, err := impExt.SourceId.Int64()
	if err != nil {
		return 0, &errortypes.BadInput{
			Message: fmt.Sprintf("ignoring imp id=%s, source parsing err: %s", imp.ID, err),
		}
	}
	return int(source), nil
}

// Builder builds a new instance of the bidmatic adapter for the given bidder with the given config.
func Builder(bidderName openrtb_ext.BidderName, config config.Adapter, server config.Server) (adapters.Bidder, error) {
	return &Adapter{
		endpoint: config.Endpoint,
	}, nil
}
