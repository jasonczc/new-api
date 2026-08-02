package fishaudio

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// convertVoiceDesignRequest forwards the client JSON body as-is, minus the
// gateway-only `model` field (Fish Audio takes the model from the header).
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/voice-design
func convertVoiceDesignRequest(c *gin.Context) (io.Reader, error) {
	var payload map[string]any
	if err := common.UnmarshalBodyReusable(c, &payload); err != nil {
		return nil, fmt.Errorf("error parsing voice design request: %w", err)
	}
	delete(payload, "model")
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshalling voice design request: %w", err)
	}
	return bytes.NewReader(jsonData), nil
}

func handleVoiceDesignResponse(c *gin.Context, resp *http.Response) (usage any, err *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// billed per successful request via ModelPrice; token counts are informational
	return &dto.Usage{PromptTokens: 1, TotalTokens: 1}, nil
}
