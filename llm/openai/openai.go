// Package openai provides a wrapper around the OpenAI API.
package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/henomis/lingoose/chat"
	"github.com/henomis/lingoose/types"
	"github.com/mitchellh/mapstructure"
	"github.com/sashabaranov/go-openai"
)

var (
	ErrOpenAICompletion = fmt.Errorf("openai completion error")
	ErrOpenAIChat       = fmt.Errorf("openai chat error")
)

const (
	DefaultOpenAIMaxTokens   = 256
	DefaultOpenAITemperature = 0.7
	DefaultOpenAINumResults  = 1
	DefaultOpenAITopP        = 1.0
	DefaultMaxIterations     = 3
)

type Model string

const (
	GPT432K0613             Model = openai.GPT432K0613
	GPT432K0314             Model = openai.GPT432K0314
	GPT432K                 Model = openai.GPT432K
	GPT40613                Model = openai.GPT40613
	GPT40314                Model = openai.GPT40314
	GPT4                    Model = openai.GPT4
	GPT3Dot5Turbo0613       Model = openai.GPT3Dot5Turbo0613
	GPT3Dot5Turbo0301       Model = openai.GPT3Dot5Turbo0301
	GPT3Dot5Turbo16K        Model = openai.GPT3Dot5Turbo16K
	GPT3Dot5Turbo16K0613    Model = openai.GPT3Dot5Turbo16K0613
	GPT3Dot5Turbo           Model = openai.GPT3Dot5Turbo
	GPT3TextDavinci003      Model = openai.GPT3TextDavinci003
	GPT3TextDavinci002      Model = openai.GPT3TextDavinci002
	GPT3TextCurie001        Model = openai.GPT3TextCurie001
	GPT3TextBabbage001      Model = openai.GPT3TextBabbage001
	GPT3TextAda001          Model = openai.GPT3TextAda001
	GPT3TextDavinci001      Model = openai.GPT3TextDavinci001
	GPT3DavinciInstructBeta Model = openai.GPT3DavinciInstructBeta
	GPT3Davinci             Model = openai.GPT3Davinci
	GPT3CurieInstructBeta   Model = openai.GPT3CurieInstructBeta
	GPT3Curie               Model = openai.GPT3Curie
	GPT3Ada                 Model = openai.GPT3Ada
	GPT3Babbage             Model = openai.GPT3Babbage
)

type OpenAIUsageCallback func(types.Meta)
type OpenAIStreamCallback func(string)

type OpenAI struct {
	openAIClient           *openai.Client
	model                  Model
	temperature            float32
	maxTokens              int
	stop                   []string
	verbose                bool
	usageCallback          OpenAIUsageCallback
	functions              map[string]Function
	functionsMaxIterations uint
	calledFunctionName     *string
	finishReason           string
}

func New(model Model, temperature float32, maxTokens int, verbose bool) *OpenAI {

	openAIKey := os.Getenv("OPENAI_API_KEY")

	return &OpenAI{
		openAIClient:           openai.NewClient(openAIKey),
		model:                  model,
		temperature:            temperature,
		maxTokens:              maxTokens,
		verbose:                verbose,
		functions:              make(map[string]Function),
		functionsMaxIterations: DefaultMaxIterations,
	}
}

func (o *OpenAI) WithModel(model Model) *OpenAI {
	o.model = model
	return o
}

func (o *OpenAI) WithTemperature(temperature float32) *OpenAI {
	o.temperature = temperature
	return o
}

func (o *OpenAI) WithMaxTokens(maxTokens int) *OpenAI {
	o.maxTokens = maxTokens
	return o
}

func (o *OpenAI) WithCallback(callback OpenAIUsageCallback) *OpenAI {
	o.usageCallback = callback
	return o
}

func (o *OpenAI) WithStop(stop []string) *OpenAI {
	o.stop = stop
	return o
}

func (o *OpenAI) WithClient(client *openai.Client) *OpenAI {
	o.openAIClient = client
	return o
}

func (o *OpenAI) WithVerbose(verbose bool) *OpenAI {
	o.verbose = verbose
	return o
}

func (o *OpenAI) WithFunctionCallMaxIterations(maxIterations uint) *OpenAI {
	o.functionsMaxIterations = maxIterations
	return o
}

func (o *OpenAI) CalledFunctionName() *string {
	return o.calledFunctionName
}

func (o *OpenAI) FinishReason() string {
	return o.finishReason
}

func NewCompletion() *OpenAI {
	return New(
		GPT3TextDavinci003,
		DefaultOpenAITemperature,
		DefaultOpenAIMaxTokens,
		false,
	)
}

func NewChat() *OpenAI {
	return New(
		GPT3Dot5Turbo,
		DefaultOpenAITemperature,
		DefaultOpenAIMaxTokens,
		false,
	)
}

func (o *OpenAI) Completion(ctx context.Context, prompt string) (string, error) {

	response, err := o.openAIClient.CreateCompletion(
		ctx,
		openai.CompletionRequest{
			Model:       string(o.model),
			Prompt:      prompt,
			MaxTokens:   o.maxTokens,
			Temperature: o.temperature,
			N:           DefaultOpenAINumResults,
			TopP:        DefaultOpenAITopP,
			Stop:        o.stop,
		},
	)

	if err != nil {
		return "", fmt.Errorf("%s: %w", ErrOpenAICompletion, err)
	}

	if o.usageCallback != nil {
		o.setUsageMetadata(response.Usage)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("%s: no choices returned", ErrOpenAICompletion)
	}

	output := strings.TrimSpace(response.Choices[0].Text)
	if o.verbose {
		debugCompletion(prompt, output)
	}

	return output, nil
}

func (o *OpenAI) CompletionStream(ctx context.Context, callbackFn OpenAIStreamCallback, prompt string) error {

	stream, err := o.openAIClient.CreateCompletionStream(
		ctx,
		openai.CompletionRequest{
			Model:       string(o.model),
			Prompt:      prompt,
			MaxTokens:   o.maxTokens,
			Temperature: o.temperature,
			N:           DefaultOpenAINumResults,
			TopP:        DefaultOpenAITopP,
			Stop:        o.stop,
		},
	)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrOpenAICompletion, err)
	}

	defer stream.Close()

	for {

		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("%s: %w", ErrOpenAICompletion, err)
		}

		if o.usageCallback != nil {
			o.setUsageMetadata(response.Usage)
		}

		if len(response.Choices) == 0 {
			return fmt.Errorf("%s: no choices returned", ErrOpenAICompletion)
		}

		output := response.Choices[0].Text
		if o.verbose {
			debugCompletion(prompt, output)
		}

		callbackFn(output)

	}

	return nil
}

func (o *OpenAI) Chat(ctx context.Context, prompt *chat.Chat) (string, error) {

	messages, err := buildMessages(prompt)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ErrOpenAIChat, err)
	}

	chatCompletionRequest := openai.ChatCompletionRequest{
		Model:       string(o.model),
		Messages:    messages,
		MaxTokens:   o.maxTokens,
		Temperature: o.temperature,
		N:           DefaultOpenAINumResults,
		TopP:        DefaultOpenAITopP,
		Stop:        o.stop,
	}

	if len(o.functions) > 0 {
		chatCompletionRequest.Functions = o.getFunctions()
	}

	response, err := o.openAIClient.CreateChatCompletion(
		ctx,
		chatCompletionRequest,
	)

	if err != nil {
		return "", fmt.Errorf("%s: %w", ErrOpenAIChat, err)
	}

	if o.usageCallback != nil {
		o.setUsageMetadata(response.Usage)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("%s: no choices returned", ErrOpenAIChat)
	}

	content := response.Choices[0].Message.Content

	o.finishReason = string(response.Choices[0].FinishReason)
	o.calledFunctionName = nil
	if response.Choices[0].FinishReason == "function_call" && len(o.functions) > 0 {
		if o.verbose {
			fmt.Printf("Calling function %s\n", response.Choices[0].Message.FunctionCall.Name)
			fmt.Printf("Function call arguments: %s\n", response.Choices[0].Message.FunctionCall.Arguments)
		}

		content, err = o.functionCall(response)
		if err != nil {
			return "", fmt.Errorf("%s: %w", ErrOpenAIChat, err)
		}
	}

	if o.verbose {
		debugChat(prompt, content)
	}

	return content, nil
}

func (o *OpenAI) ChatStream(ctx context.Context, callbackFn OpenAIStreamCallback, prompt *chat.Chat) error {

	messages, err := buildMessages(prompt)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrOpenAIChat, err)
	}

	stream, err := o.openAIClient.CreateChatCompletionStream(
		ctx,
		openai.ChatCompletionRequest{
			Model:       string(o.model),
			Messages:    messages,
			MaxTokens:   o.maxTokens,
			Temperature: o.temperature,
			N:           DefaultOpenAINumResults,
			TopP:        DefaultOpenAITopP,
			Stop:        o.stop,
		},
	)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrOpenAIChat, err)
	}

	for {

		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		// oops no usage here?
		// if o.usageCallback != nil {
		// 	o.setUsageMetadata(response.Usage)
		// }

		if len(response.Choices) == 0 {
			return fmt.Errorf("%s: no choices returned", ErrOpenAIChat)
		}

		content := response.Choices[0].Delta.Content

		if o.verbose {
			debugChat(prompt, content)
		}

		callbackFn(content)
	}

	return nil
}

func (o *OpenAI) SetStop(stop []string) {
	o.stop = stop
}

func (o *OpenAI) setUsageMetadata(usage openai.Usage) {

	callbackMetadata := make(types.Meta)

	err := mapstructure.Decode(usage, &callbackMetadata)
	if err != nil {
		return
	}

	o.usageCallback(callbackMetadata)
}

func buildMessages(prompt *chat.Chat) ([]openai.ChatCompletionMessage, error) {

	var messages []openai.ChatCompletionMessage

	promptMessages, err := prompt.ToMessages()
	if err != nil {
		return nil, err
	}

	for _, message := range promptMessages {
		if message.Type == chat.MessageTypeUser {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: message.Content,
			})
		} else if message.Type == chat.MessageTypeAssistant {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: message.Content,
			})
		} else if message.Type == chat.MessageTypeSystem {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: message.Content,
			})
		} else if message.Type == chat.MessageTypeFunction {

			fnmessage := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleFunction,
				Content: message.Content,
			}
			if message.Name != nil {
				fnmessage.Name = *message.Name
			}

			messages = append(messages, fnmessage)
		}
	}

	return messages, nil
}

func debugChat(prompt *chat.Chat, content string) {

	promptMessages, err := prompt.ToMessages()
	if err != nil {
		return
	}

	for _, message := range promptMessages {
		if message.Type == chat.MessageTypeUser {
			fmt.Printf("---USER---\n%s\n", message.Content)
		} else if message.Type == chat.MessageTypeAssistant {
			fmt.Printf("---AI---\n%s\n", message.Content)
		} else if message.Type == chat.MessageTypeSystem {
			fmt.Printf("---SYSTEM---\n%s\n", message.Content)
		} else if message.Type == chat.MessageTypeFunction {
			fmt.Printf("---FUNCTION---\n%s()\n%s\n", *message.Name, message.Content)
		}
	}
	fmt.Printf("---AI---\n%s\n", content)
}

func debugCompletion(prompt string, content string) {
	fmt.Printf("---USER---\n%s\n", prompt)
	fmt.Printf("---AI---\n%s\n", content)
}
