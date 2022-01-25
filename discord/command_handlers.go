package discord

import (
	"fmt"
	"github.com/automuteus/utils/pkg/settings"
	"log"
	"regexp"
	"strings"

	"github.com/denverquane/amongusdiscord/metrics"

	"github.com/bwmarrin/discordgo"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

const (
	MaxDebugMessageSize = 1980
	detailedMapString   = "detailed"
	clearArgumentString = "clear"
	trueString          = "true"
)

var MatchIDRegex = regexp.MustCompile(`^[A-Z0-9]{8}:[0-9]+$`)

// TODO cache/preconstruct these (no reason to make them fresh everytime help is called, except for the prefix...)
func ConstructEmbedForCommand(
	prefix string,
	cmd Command,
	sett *settings.GuildSettings,
) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		URL:   "",
		Type:  "",
		Title: cmd.Emoji + " " + strings.Title(cmd.Command),
		Description: sett.LocalizeMessage(cmd.Description,
			map[string]interface{}{
				"CommandPrefix": sett.GetCommandPrefix(),
			}),
		Timestamp: "",
		Color:     15844367, // GOLD
		Image:     nil,
		Thumbnail: nil,
		Video:     nil,
		Provider:  nil,
		Author:    nil,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: sett.LocalizeMessage(&i18n.Message{
					ID:    "commands.ConstructEmbedForCommand.Fields.Example",
					Other: "Example",
				}),
				Value:  "`" + fmt.Sprintf("%s %s", sett.GetCommandPrefix(), cmd.Example) + "`",
				Inline: false,
			},
			{
				Name: sett.LocalizeMessage(&i18n.Message{
					ID:    "commands.ConstructEmbedForCommand.Fields.Arguments",
					Other: "Arguments",
				}),
				Value:  "`" + sett.LocalizeMessage(cmd.Arguments) + "`",
				Inline: false,
			},
			{
				Name: sett.LocalizeMessage(&i18n.Message{
					ID:    "commands.ConstructEmbedForCommand.Fields.Aliases",
					Other: "Aliases",
				}),
				Value:  strings.Join(cmd.Aliases, ", "),
				Inline: false,
			},
		},
	}
}

type commandRequest struct {
	isAdmin        bool
	isPermissioned bool
	sett           *settings.GuildSettings
	session        *discordgo.Session
	guild          *discordgo.Guild
	message        *discordgo.MessageCreate
	args           []string
}

func (bot *Bot) HandleCommand(
	isAdmin bool,
	isPermissioned bool,
	sett *settings.GuildSettings,
	session *discordgo.Session,
	guild *discordgo.Guild,
	message *discordgo.MessageCreate,
	args []string,
) {
	command, exists := getCommand(args[0])

	if !exists {
		log.Print(fmt.Sprintf("\"%s\" command typed by User %s\n", command.Command, message.Author.ID))
		session.ChannelMessageSend(
			message.ChannelID,
			sett.LocalizeMessage(
				&i18n.Message{
					ID:    "commands.HandleCommand.default",
					Other: "Sorry, I didn't understand `{{.InvalidCommand}}`! Please see `{{.CommandPrefix}} help` for commands",
				},
				map[string]interface{}{
					"CommandPrefix":  sett.CommandPrefix,
					"InvalidCommand": args[0],
				},
			),
		)
		return
	}

	if command.IsAdmin && !isAdmin {
		session.ChannelMessageSend(message.ChannelID, sett.LocalizeMessage(&i18n.Message{
			ID:    "message_handlers.handleMessageCreate.noPerms",
			Other: "User does not have the required permissions to execute this command!",
		}))
		return
	}

	// admins can invoke moderator commands
	if command.IsOperator && (!isPermissioned && !isAdmin) {
		session.ChannelMessageSend(message.ChannelID, sett.LocalizeMessage(&i18n.Message{
			ID:    "message_handlers.handleMessageCreate.noPerms",
			Other: "User does not have the required permissions to execute this command!",
		}))
		return
	}

	metrics.RecordDiscordRequests(bot.RedisInterface.client, metrics.MessageCreateDelete, 2)
	channelID, msgToSend := command.fn(bot, isAdmin, isPermissioned, sett, guild, message, args, &command)
	switch msgToSend.(type) {
	case string:
		session.ChannelMessageSend(channelID, msgToSend.(string))
	case []string:
		for _, v := range msgToSend.([]string) {
			session.ChannelMessageSend(channelID, v)
		}
	case discordgo.MessageEmbed:
		embed := msgToSend.(discordgo.MessageEmbed)
		session.ChannelMessageSendEmbed(channelID, &embed)
	case *discordgo.MessageEmbed:
		session.ChannelMessageSendEmbed(channelID, msgToSend.(*discordgo.MessageEmbed))
	case nil:
		// do nothing
	default:
		log.Printf("Incapable of processing sendMessage of type: %T", msgToSend)
	}
	deleteMessage(session, message.ChannelID, message.Message.ID)
}
