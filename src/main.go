package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"os/signal"
	"src/aws"
	"src/discord"
	"src/openai"
	"syscall"
	"time"
)

const (
	discordTokenEnvName  = "DISCORD_TOKEN"
	openaiTokenEnvName   = "OPENAI_TOKEN"
	guildIDTokenEnvName  = "DISCORD_GUILD_ID"
	lockTableNameEnvName = "LOCK_TABLE_NAME"
	awsRegionEnvName     = "AWS_REGION"
)

var (
	lockMaxShards                = 2
	lockLeaseDurationSeconds     = 10
	lockHeartbeatIntervalSeconds = 3
)

type LockData struct {
	MessageID string `json:"message_id"`
}

func (l LockData) Marshal() ([]byte, error) {
	return json.Marshal(l)
}

func (l LockData) Unmarshal(data []byte) error {
	return json.Unmarshal(data, &l)
}

func getLockClient(zlog *zerolog.Logger) (aws.LockClient, error) {
	// Get a host identifier, which is a concatenation of the hostname and the process ID.
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	hostIdentifier := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	lockTableName, ok := os.LookupEnv(lockTableNameEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", lockTableNameEnvName)
	}
	awsRegion, ok := os.LookupEnv(awsRegionEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", awsRegionEnvName)

	}
	config := aws.DynamoDBLockConfig{
		Owner:                    hostIdentifier,
		MaxShards:                lockMaxShards,
		LeaseDurationSeconds:     lockLeaseDurationSeconds,
		HeartbeatIntervalSeconds: lockHeartbeatIntervalSeconds,
	}

	dynamodbLockClient, err := aws.NewDynamoDBLockClient(
		lockTableName,
		awsRegion,
		config,
		zlog,
	)
	if err != nil {
		return nil, err
	}
	return dynamodbLockClient, nil
}

func main() {
	zlog := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.TimeFieldFormat = time.RFC3339Nano

	openaiToken, ok := os.LookupEnv(openaiTokenEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", openaiTokenEnvName)
	}
	openaiClient := openai.NewOpenAI(openaiToken)
	defer func(openaiClient *openai.OpenAI) {
		err := openaiClient.Close(&zlog)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to close openai client")
		}
	}(openaiClient)

	lockClient, err := getLockClient(&zlog)
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to create lock client")
	}
	defer func(lockClient aws.LockClient) {
		zlog.Info().Msg("Closing lock client")
		err := lockClient.Close()
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to close lock client")
		}
	}(lockClient)

	// TODO REMOVEME do a test acquire of a lock
	_, err = lockClient.Acquire(context.TODO(), "test", LockData{MessageID: "message-id"})
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to acquire lock")
	}

	discordToken, ok := os.LookupEnv(discordTokenEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", discordTokenEnvName)
	}
	guildID, ok := os.LookupEnv(guildIDTokenEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", guildIDTokenEnvName)
	}

	discordBot, err := discord.NewDiscord(discordToken, openaiClient, guildID, &zlog)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func(discordBot *discord.Discord) {
		zlog.Info().Msg("Closing discord bot")
		err := discordBot.Close(&zlog)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to close discord bot")
		}
	}(discordBot)

	zlog.Info().Msg("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	zlog.Info().Msg("Bot is now exiting.")
}
