package main

import (
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"os/signal"
	"src/discord"
	"src/openai"
	"syscall"
	"time"
)

const (
	discordTokenEnvName = "DISCORD_TOKEN"
	openaiTokenEnvName  = "OPENAI_TOKEN"
	guildIDTokenEnvName = "DISCORD_GUILD_ID"
)

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
