/*
 * Copyright (C) 2023 Asim Ihsan
 * SPDX-License-Identifier: AGPL-3.0-only
 *
 * This program is free software: you can redistribute it and/or modify it under
 * the terms of the GNU Affero General Public License as published by the Free
 * Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License along
 * with this program. If not, see <https://www.gnu.org/licenses/>
 */

package main

import (
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
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
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zlog = zlog.Level(zerolog.DebugLevel).With().Caller().Logger()

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

	discordToken, ok := os.LookupEnv(discordTokenEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", discordTokenEnvName)
	}
	guildID, ok := os.LookupEnv(guildIDTokenEnvName)
	if !ok {
		zlog.Fatal().Msgf("Missing %s environment variable", guildIDTokenEnvName)
	}

	discordBot, err := discord.NewDiscord(
		discordToken,
		openaiClient,
		lockClient,
		guildID,
		&zlog)
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
