package command

import (
	"github.com/automuteus/utils/pkg/settings"
	"github.com/bwmarrin/discordgo"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

var End = discordgo.ApplicationCommand{
	Name:        "end",
	Description: "End a game",
}

func EndResponse(sett *settings.GuildSettings) *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: 1 << 6,
			Content: sett.LocalizeMessage(&i18n.Message{
				ID:    "commands.end",
				Other: "I've received your request to end the game",
			}),
		},
	}
}