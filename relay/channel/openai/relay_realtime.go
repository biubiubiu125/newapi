package openai

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	sendChan := make(chan []byte, 100)
	receiveChan := make(chan []byte, 100)
	errChan := make(chan error, 2)

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	var usageMu sync.Mutex

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in client reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from client: %v", err)
					}
					close(clientClosed)
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					errChan <- fmt.Errorf("error counting text token: %v", err)
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				usageMu.Lock()
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken
				usageMu.Unlock()

				err = helper.WssString(c, targetConn, string(message))
				if err != nil {
					errChan <- fmt.Errorf("error writing to target: %v", err)
					return
				}

				select {
				case sendChan <- message:
				default:
				}
			}
		}
	})

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in target reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from target: %v", err)
					}
					close(targetClosed)
					return
				}
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				var commitAfterDelivery func() error
				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					realtimeUsage := realtimeEvent.Response.Usage
					if realtimeUsage != nil {
						usageMu.Lock()
						usage.TotalTokens += realtimeUsage.TotalTokens
						usage.InputTokens += realtimeUsage.InputTokens
						usage.OutputTokens += realtimeUsage.OutputTokens
						usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
						usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
						usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
						usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
						usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
						snapshot := usage
						usage = &dto.RealtimeUsage{}
						localUsage = &dto.RealtimeUsage{}
						usageMu.Unlock()
						commitAfterDelivery = func() error {
							return commitRealtimeUsageLocked(true, snapshot, sumUsage, &usageMu, func() error {
								return preConsumeUsage(c, info, snapshot, sumUsage, &usageMu)
							})
						}
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							errChan <- fmt.Errorf("error counting text token: %v", err)
							return
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						usageMu.Lock()
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						snapshot := localUsage
						localUsage = &dto.RealtimeUsage{}
						usageMu.Unlock()
						commitAfterDelivery = func() error {
							return commitRealtimeUsageLocked(true, snapshot, sumUsage, &usageMu, func() error {
								return preConsumeUsage(c, info, snapshot, sumUsage, &usageMu)
							})
						}
					}
					logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						errChan <- fmt.Errorf("error counting text token: %v", err)
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					pendingText := textToken
					pendingAudio := audioToken
					commitAfterDelivery = func() error {
						usageMu.Lock()
						localUsage.TotalTokens += pendingText + pendingAudio
						localUsage.OutputTokens += pendingText + pendingAudio
						localUsage.OutputTokenDetails.TextTokens += pendingText
						localUsage.OutputTokenDetails.AudioTokens += pendingAudio
						usageMu.Unlock()
						return nil
					}
				}

				err = helper.WssString(c, clientConn, string(message))
				if err != nil {
					errChan <- fmt.Errorf("error writing to client: %v", err)
					return
				}
				info.MarkClientStreamWrite()
				if commitAfterDelivery != nil {
					if err = commitAfterDelivery(); err != nil {
						errChan <- fmt.Errorf("error consume usage: %v", err)
						return
					}
				}

				select {
				case receiveChan <- message:
				default:
				}
			}
		}
	})

	select {
	case <-clientClosed:
	case <-targetClosed:
	case err := <-errChan:
		//return service.OpenAIErrorWrapper(err, "realtime_error", http.StatusInternalServerError), nil
		logger.LogError(c, "realtime error: "+err.Error())
	case <-c.Done():
	}

	usageMu.Lock()
	pendingUsage := usage
	pendingLocal := localUsage
	usage = &dto.RealtimeUsage{}
	localUsage = &dto.RealtimeUsage{}
	usageMu.Unlock()
	if err := finalizeRealtimeUsage(pendingUsage, pendingLocal, sumUsage, &usageMu, func(item *dto.RealtimeUsage) error {
		return preConsumeUsage(c, info, item, sumUsage, &usageMu)
	}); err != nil {
		logger.LogError(c, fmt.Sprintf("realtime final reserve failed: %v", err))
	}

	return nil, sumUsage
}

// finalizeRealtimeUsage flushes whatever was still unbilled when the socket ended.
func finalizeRealtimeUsage(usage *dto.RealtimeUsage, local *dto.RealtimeUsage, sum *dto.RealtimeUsage, mu *sync.Mutex, reserve func(*dto.RealtimeUsage) error) error {
	var first error
	for _, item := range []*dto.RealtimeUsage{usage, local} {
		if item == nil || item.TotalTokens == 0 {
			continue
		}
		err := commitRealtimeUsageLocked(true, item, sum, mu, func() error {
			if reserve == nil {
				return nil
			}
			return reserve(item)
		})
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage, mu *sync.Mutex) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	if err := service.PreWssConsumeQuota(ctx, info, usage); err != nil {
		return err
	}
	addRealtimeUsageLocked(mu, totalUsage, usage)
	return nil
}

// commitRealtimeUsageAfterDelivery bills a frame only after the client has it.
// A failed reserve still adds the usage so the final settlement can charge the delivered frame.
// A failed client write must not call this.
func commitRealtimeUsageAfterDelivery(delivered bool, usage *dto.RealtimeUsage, total *dto.RealtimeUsage, reserve func() error) error {
	return commitRealtimeUsageLocked(delivered, usage, total, nil, reserve)
}

func commitRealtimeUsageLocked(delivered bool, usage *dto.RealtimeUsage, total *dto.RealtimeUsage, mu *sync.Mutex, reserve func() error) error {
	if !delivered || usage == nil {
		return nil
	}
	if reserve != nil {
		if err := reserve(); err != nil {
			addRealtimeUsageLocked(mu, total, usage)
			return err
		}
		return nil
	}
	addRealtimeUsageLocked(mu, total, usage)
	return nil
}

func addRealtimeUsageLocked(mu *sync.Mutex, total *dto.RealtimeUsage, usage *dto.RealtimeUsage) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	addRealtimeUsage(total, usage)
}

func addRealtimeUsage(total *dto.RealtimeUsage, usage *dto.RealtimeUsage) {
	if total == nil || usage == nil {
		return
	}
	total.TotalTokens += usage.TotalTokens
	total.InputTokens += usage.InputTokens
	total.OutputTokens += usage.OutputTokens
	total.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	total.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	total.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	total.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	total.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
}
