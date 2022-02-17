package discord

import (
	"github.com/automuteus/automuteus/discord/command"
	"github.com/bwmarrin/discordgo"
	"log"
)

func (bot *Bot) handleInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// lock this particular interaction message so no other shard tries to process it
	interactionLock := bot.RedisInterface.LockSnowflake(i.ID)
	// couldn't obtain lock; bail bail bail!
	if interactionLock == nil {
		return
	}
	defer interactionLock.Release(ctx)

	sett := bot.StorageInterface.GetGuildSettings(i.GuildID)

	var response *discordgo.InteractionResponse

	switch i.ApplicationCommandData().Name {
	case "help":
		response = command.HelpResponse(sett, i.ApplicationCommandData().Options)

	case "info":
		botInfo := bot.getInfo()
		response = command.InfoResponse(botInfo, i.GuildID, sett)

	case "link":
		userID, colorOrName := command.GetLinkParams(s, i.ApplicationCommandData().Options)
		gsr := GameStateRequest{
			GuildID:     i.GuildID,
			TextChannel: i.ChannelID,
		}
		lock, dgs := bot.RedisInterface.GetDiscordGameStateAndLock(gsr)
		if lock == nil {
			log.Printf("No lock could be obtained when linking for guild %s, channel %s\n", i.GuildID, i.ChannelID)
			// TODO more gracefully respond
			return
		}

		status, err := bot.linkPlayer(dgs, userID, colorOrName)
		if err != nil {
			log.Println(err)
		}
		if status == command.LinkSuccess {
			bot.RedisInterface.SetDiscordGameState(dgs, lock)
			dgs.Edit(bot.PrimarySession, bot.gameStateResponse(dgs, sett))
		} else {
			// release the lock
			bot.RedisInterface.SetDiscordGameState(nil, lock)
		}
		response = command.LinkResponse(status, userID, colorOrName, sett)

	case "unlink":
		userID := command.GetUnlinkParams(s, i.ApplicationCommandData().Options)
		gsr := GameStateRequest{
			GuildID:     i.GuildID,
			TextChannel: i.ChannelID,
		}
		lock, dgs := bot.RedisInterface.GetDiscordGameStateAndLock(gsr)
		if lock == nil {
			log.Printf("No lock could be obtained when unlinking for guild %s, channel %s\n", i.GuildID, i.ChannelID)
			return
		}

		status, err := bot.unlinkPlayer(dgs, userID)
		if err != nil {
			log.Println(err)
		}
		if status == command.UnlinkSuccess {
			bot.RedisInterface.SetDiscordGameState(dgs, lock)
			dgs.Edit(bot.PrimarySession, bot.gameStateResponse(dgs, sett))
		} else {
			// release the lock
			bot.RedisInterface.SetDiscordGameState(nil, lock)
		}
		response = command.UnlinkResponse(status, userID, sett)

	case "new":
		gsr := GameStateRequest{
			GuildID:     i.GuildID,
			TextChannel: i.ChannelID,
		}
		lock, dgs := bot.RedisInterface.GetDiscordGameStateAndLock(gsr)
		if lock == nil {
			// TODO use retries like original new command
			log.Printf("No lock could be obtained when making a new game for guild %s, channel %s\n", i.GuildID, i.ChannelID)
			return
		}
		g, err := s.State.Guild(i.GuildID)
		if err != nil {
			log.Println(err)
			return
		}

		tracking := bot.getTrackingChannel(g, i.Member.User.ID)

		status, activeGames := bot.newGame(dgs, tracking)
		if status == command.NewSuccess {
			// release the lock
			bot.RedisInterface.SetDiscordGameState(dgs, lock)

			bot.RedisInterface.RefreshActiveGame(dgs.GuildID, dgs.ConnectCode)

			killChan := make(chan EndGameMessage)

			go bot.SubscribeToGameByConnectCode(i.GuildID, dgs.ConnectCode, killChan)

			bot.ChannelsMapLock.Lock()
			bot.EndGameChannels[dgs.ConnectCode] = killChan
			bot.ChannelsMapLock.Unlock()

			hyperlink, minimalURL := formCaptureURL(bot.url, dgs.ConnectCode)
			response = command.NewResponse(status, command.NewInfo{
				Hyperlink:   hyperlink,
				MinimalURL:  minimalURL,
				ConnectCode: dgs.ConnectCode,
				ActiveGames: activeGames, // not actually needed for Success messages
			}, sett)

			bot.handleGameStartMessage(i.GuildID, i.ChannelID, i.Member.User.ID, sett, tracking, g, dgs.ConnectCode)
		} else {
			// release the lock
			bot.RedisInterface.SetDiscordGameState(nil, lock)
			response = command.NewResponse(status, command.NewInfo{
				ActiveGames: activeGames, // only field we need for success messages
			}, sett)
		}
	}
	if response != nil {
		err := s.InteractionRespond(i.Interaction, response)
		if err != nil {
			log.Println(err)
		}
	}
}
