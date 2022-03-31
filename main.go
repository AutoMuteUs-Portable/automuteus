package main

import (
	"errors"
	"github.com/automuteus/automuteus/discord/command"
	"github.com/automuteus/utils/pkg/locale"
	storage2 "github.com/automuteus/utils/pkg/storage"
	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/automuteus/automuteus/storage"

	"github.com/automuteus/automuteus/discord"
	"github.com/rs/zerolog/log"
)

var (
	version = "7.0.0"
	commit  = "none"
	date    = "unknown"
)

const DefaultURL = "http://localhost:8123"

type registeredCommand struct {
	GuildID            string
	ApplicationCommand *discordgo.ApplicationCommand
}

func main() {
	// seed the rand generator (used for making connection codes)
	rand.Seed(time.Now().Unix())
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.With().Caller().Logger()
	err := discordMainWrapper()
	if err != nil {
		log.Fatal().Err(err)
	}
}

func discordMainWrapper() error {
	var isOfficial = os.Getenv("AUTOMUTEUS_OFFICIAL") != ""

	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	if discordToken == "" {
		return errors.New("no DISCORD_BOT_TOKEN provided")
	}
	logPath := os.Getenv("LOG_PATH")
	if logPath == "" {
		logPath = "./"
	}

	logEntry := os.Getenv("DISABLE_LOG_FILE")
	if logEntry == "" {
		file, err := os.Create(path.Join(logPath, "logs.txt"))
		if err != nil {
			return err
		}
		mw := io.MultiWriter(os.Stdout, file)
		log.Logger = zerolog.New(mw).With().Caller().Logger()
	}

	emojiGuildID := os.Getenv("EMOJI_GUILD_ID")

	log.Info().Str("version", version).Str("commit", commit)

	if os.Getenv("WORKER_BOT_TOKENS") != "" {
		log.Fatal().Msg("WORKER_BOT_TOKENS is now a variable used by Galactus, not AutoMuteUs!")
	}

	numShardsStr := os.Getenv("NUM_SHARDS")
	numShards, err := strconv.Atoi(numShardsStr)
	if err != nil {
		numShards = 1
	}
	log.Info().Int("NUM_SHARDS", numShards)

	shardIDStr := os.Getenv("SHARD_ID")
	shardID, err := strconv.Atoi(shardIDStr)
	if shardID >= numShards {
		return errors.New("you specified a shardID higher than or equal to the total number of shards")
	}
	if err != nil {
		shardID = 0
	}
	log.Info().Int("SHARD_ID", shardID)

	url := os.Getenv("HOST")
	if url == "" {
		log.Printf("[Info] No valid HOST provided. Defaulting to %s\n", DefaultURL)
		url = DefaultURL
	}

	var redisClient discord.RedisInterface
	var storageInterface storage.StorageInterface

	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASS")
	if redisAddr != "" {
		err := redisClient.Init(storage.RedisParameters{
			Addr:     redisAddr,
			Username: "",
			Password: redisPassword,
		})
		if err != nil {
			log.Error().Err(err)
		}
		err = storageInterface.Init(storage.RedisParameters{
			Addr:     redisAddr,
			Username: "",
			Password: redisPassword,
		})
		if err != nil {
			log.Error().Err(err)
		}
	} else {
		return errors.New("no REDIS_ADDR specified; exiting")
	}

	galactusAddr := os.Getenv("GALACTUS_ADDR")
	if galactusAddr == "" {
		return errors.New("no GALACTUS_ADDR specified; exiting")
	}

	galactusClient, err := discord.NewGalactusClient(galactusAddr)
	if err != nil {
		log.Error().Err(err)
		return err
	}

	locale.InitLang(os.Getenv("LOCALE_PATH"), os.Getenv("BOT_LANG"))

	psql := storage2.PsqlInterface{}
	pAddr := os.Getenv("POSTGRES_ADDR")
	if pAddr == "" {
		return errors.New("no POSTGRES_ADDR specified; exiting")
	}

	pUser := os.Getenv("POSTGRES_USER")
	if pUser == "" {
		return errors.New("no POSTGRES_USER specified; exiting")
	}

	pPass := os.Getenv("POSTGRES_PASS")
	if pPass == "" {
		return errors.New("no POSTGRES_PASS specified; exiting")
	}

	err = psql.Init(storage2.ConstructPsqlConnectURL(pAddr, pUser, pPass))
	if err != nil {
		return err
	}

	if !isOfficial {
		go func() {
			err := psql.LoadAndExecFromFile("./storage/postgres.sql")
			if err != nil {
				log.Fatal().Err(err)
			}
		}()
	}

	log.Info().Msg("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	bot := discord.MakeAndStartBot(version, commit, discordToken, url, emojiGuildID, numShards, shardID, &redisClient, &storageInterface, &psql, galactusClient, logPath)
	if bot == nil {
		log.Fatal().Msg("Bot failed to initialize; did you provide a valid Discord Bot Token?")
	}

	// empty string entry = global
	slashCommandGuildIds := []string{""}
	slashCommandGuildIdStr := strings.ReplaceAll(os.Getenv("SLASH_COMMAND_GUILD_IDS"), " ", "")
	if slashCommandGuildIdStr != "" {
		slashCommandGuildIds = strings.Split(slashCommandGuildIdStr, ",")
	}

	var registeredCommands []registeredCommand
	if !isOfficial || shardID == 0 {
		for _, guild := range slashCommandGuildIds {
			for _, v := range command.All {
				if guild == "" {
					log.Info().Str("command", v.Name).Str("guild", "GLOBAL").Msg("register command")
				} else {
					log.Info().Str("command", v.Name).Str("guild", guild).Msg("register command")
				}

				id, err := bot.PrimarySession.ApplicationCommandCreate(bot.PrimarySession.State.User.ID, guild, v)
				if err != nil {
					log.Error().Err(err)
				} else {
					registeredCommands = append(registeredCommands, registeredCommand{
						GuildID:            guild,
						ApplicationCommand: id,
					})
				}
			}
		}
		log.Info().Msg("Finishing registering all commands!")
	}

	<-sc
	log.Printf("Received Sigterm or Kill signal. Bot will terminate in 1 second")
	time.Sleep(time.Second)

	if !isOfficial {
		log.Info().Msg("Deleting slash commands")
		for _, v := range registeredCommands {
			if v.GuildID == "" {
				log.Info().Str("command", v.ApplicationCommand.Name).Str("guild", "GLOBAL").Msg("delete command")
			} else {
				log.Info().Str("command", v.ApplicationCommand.Name).Str("guild", v.GuildID).Msg("delete command")
			}
			err = bot.PrimarySession.ApplicationCommandDelete(v.ApplicationCommand.ApplicationID, v.GuildID, v.ApplicationCommand.ID)
			if err != nil {
				log.Error().Err(err)
			}
		}
		log.Info().Msg("Finished deleting all commands")
	}

	bot.Close()
	return nil
}
