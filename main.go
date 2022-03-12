package main

import (
	"errors"
	"github.com/automuteus/automuteus/discord/command"
	"github.com/automuteus/utils/pkg/locale"
	storage2 "github.com/automuteus/utils/pkg/storage"
	"github.com/bwmarrin/discordgo"
	"io"
	"log"
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
)

var (
	version = "6.16.0"
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
	err := discordMainWrapper()
	if err != nil {
		log.Println("Program exited with the following error:")
		log.Println(err)
		return
	}
}

func discordMainWrapper() error {
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
		log.SetOutput(mw)
	}

	emojiGuildID := os.Getenv("EMOJI_GUILD_ID")

	log.Println(version + "-" + commit)

	var extraTokens []string
	extraTokenStr := strings.ReplaceAll(os.Getenv("WORKER_BOT_TOKENS"), " ", "")
	if extraTokenStr != "" {
		extraTokens = strings.Split(extraTokenStr, ",")
	}

	if len(extraTokens) > 0 {
		log.Printf("You provided %d worker tokens so I'll be sending them to Galactus\n", len(extraTokens))
	}

	numShardsStr := os.Getenv("NUM_SHARDS")
	numShards, err := strconv.Atoi(numShardsStr)
	if err != nil {
		numShards = 1
	}

	shardIDStr := os.Getenv("SHARD_ID")
	shardID, err := strconv.Atoi(shardIDStr)
	if shardID >= numShards {
		return errors.New("you specified a shardID higher than or equal to the total number of shards")
	}
	if err != nil {
		shardID = 0
	}

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
			log.Println(err)
		}
		err = storageInterface.Init(storage.RedisParameters{
			Addr:     redisAddr,
			Username: "",
			Password: redisPassword,
		})
		if err != nil {
			log.Println(err)
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
		log.Println("Error connecting to Galactus!")
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

	if os.Getenv("AUTOMUTEUS_OFFICIAL") == "" {
		go func() {
			err := psql.LoadAndExecFromFile("./storage/postgres.sql")
			if err != nil {
				log.Println("Exiting with fatal error when attempting to execute postgres.sql:")
				log.Fatal(err)
			}
		}()
	}

	log.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)

	bot := discord.MakeAndStartBot(version, commit, discordToken, url, emojiGuildID, extraTokens, numShards, shardID, &redisClient, &storageInterface, &psql, galactusClient, logPath)

	slashCommandGuildIds := []string{""}
	slashCommandGuildIdStr := strings.ReplaceAll(os.Getenv("SLASH_COMMAND_GUILD_IDS"), " ", "")
	if slashCommandGuildIdStr != "" {
		slashCommandGuildIds = strings.Split(slashCommandGuildIdStr, ",")
	}

	var registeredCommands []registeredCommand
	for _, guild := range slashCommandGuildIds {
		for _, v := range command.All {
			log.Printf("Registering command %s in guild %s\n", v.Name, guild)
			id, err := bot.PrimarySession.ApplicationCommandCreate(bot.PrimarySession.State.User.ID, guild, v)
			if err != nil {
				log.Panicf("Cannot create command: %v", err)
			} else {
				registeredCommands = append(registeredCommands, registeredCommand{
					GuildID:            guild,
					ApplicationCommand: id,
				})
			}
		}
	}

	// TODO properly detect if commands should be overwritten or created
	//registeredCommands, err := bot.PrimarySession.ApplicationCommandBulkOverwrite(bot.PrimarySession.State.User.ID, os.Getenv("SLASH_COMMAND_GUILD_ID"), command.All)
	//if err != nil {
	//	log.Fatal(err)
	//}

	<-sc
	log.Printf("Received Sigterm or Kill signal. Bot will terminate in 1 second")
	time.Sleep(time.Second)

	if os.Getenv("AUTOMUTEUS_OFFICIAL") == "" {
		log.Println("Deleting slash commands")
		for _, v := range registeredCommands {
			log.Printf("Deleting command %s on guild %s\n", v.ApplicationCommand.Name, v.GuildID)
			err = bot.PrimarySession.ApplicationCommandDelete(v.ApplicationCommand.ApplicationID, v.GuildID, v.ApplicationCommand.ID)
			if err != nil {
				log.Println(err)
			}
		}
	}

	bot.Close()
	return nil
}
