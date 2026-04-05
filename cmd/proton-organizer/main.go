package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"

	"github.com/ProtonMail/go-proton-api"
)

func main() {
	log.SetOutput(os.Stderr)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: proton-organizer <command> [args]")
		fmt.Fprintln(os.Stderr, "Commands: account-info, list-folders, list-inbox, move-message, create-folder")
		os.Exit(1)
	}

	ctx := context.Background()
	m := proton.New(
		proton.WithAppVersion(fmt.Sprintf("%s-bridge@3.15.0", runtime.GOOS)),
	)
	defer m.Close()

	c, err := authenticate(ctx, m)
	if err != nil {
		log.Fatalf("Auth failed: %v", err)
	}
	defer c.Close()

	switch os.Args[1] {
	case "account-info":
		cmdAccountInfo(ctx, c)
	case "list-folders":
		cmdListFolders(ctx, c)
	case "list-inbox":
		cmdListInbox(ctx, c)
	case "move-message":
		cmdMoveMessage(ctx, c)
	case "create-folder":
		cmdCreateFolder(ctx, c)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("JSON encode: %v", err)
	}
}

func requireFlag(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	log.Fatalf("Missing required flag: %s", flag)
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func optionalFlag(args []string, flag, defaultVal string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultVal
}

// --- account-info ---

type AccountInfoOut struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	UsedSpace   uint64   `json:"used_space"`
	MaxSpace    uint64   `json:"max_space"`
	MailUsage   uint64   `json:"mail_usage"`
	Addresses   []string `json:"addresses"`
}

func cmdAccountInfo(ctx context.Context, c *proton.Client) {
	user, err := c.GetUser(ctx)
	if err != nil {
		log.Fatalf("GetUser: %v", err)
	}

	addresses, err := c.GetAddresses(ctx)
	if err != nil {
		log.Fatalf("GetAddresses: %v", err)
	}

	out := AccountInfoOut{
		Name:        user.Name,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		UsedSpace:   user.UsedSpace,
		MaxSpace:    user.MaxSpace,
		MailUsage:   user.ProductUsedSpace.Mail,
	}
	for _, addr := range addresses {
		out.Addresses = append(out.Addresses, addr.Email)
	}

	outputJSON(out)
}

// --- list-folders ---

type FolderOut struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	ParentID string `json:"parent_id,omitempty"`
}

func cmdListFolders(ctx context.Context, c *proton.Client) {
	labels, err := c.GetLabels(ctx, proton.LabelTypeFolder, proton.LabelTypeLabel)
	if err != nil {
		log.Fatalf("GetLabels: %v", err)
	}

	var out []FolderOut
	for _, l := range labels {
		path := l.Name
		if len(l.Path) > 0 {
			path = joinPath(l.Path)
		}
		out = append(out, FolderOut{
			ID:       l.ID,
			Name:     l.Name,
			Path:     path,
			ParentID: l.ParentID,
		})
	}

	outputJSON(out)
}

func joinPath(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "/"
		}
		result += p
	}
	return result
}

// --- list-inbox ---

type MessageOut struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	CC      []string `json:"cc"`
	Subject string   `json:"subject"`
	Date    int64    `json:"date"`
	Labels  []string `json:"labels"`
	Unread  bool     `json:"unread"`
}

func cmdListInbox(ctx context.Context, c *proton.Client) {
	args := os.Args[2:]
	limitStr := optionalFlag(args, "--limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		log.Fatalf("Invalid --limit: %v", err)
	}
	filterRead := hasFlag(args, "--read")
	filterUnread := hasFlag(args, "--unread")

	msgs, err := c.GetMessageMetadata(ctx, proton.MessageFilter{LabelID: proton.InboxLabel})
	if err != nil {
		log.Fatalf("GetMessageMetadata: %v", err)
	}

	var filtered []proton.MessageMetadata
	for _, msg := range msgs {
		if filterRead && bool(msg.Unread) {
			continue
		}
		if filterUnread && !bool(msg.Unread) {
			continue
		}
		filtered = append(filtered, msg)
	}
	msgs = filtered

	if limit > 0 && limit < len(msgs) {
		msgs = msgs[:limit]
	}

	var out []MessageOut
	for _, msg := range msgs {
		m := MessageOut{
			ID:      msg.ID,
			Subject: msg.Subject,
			Date:    msg.Time,
			Labels:  msg.LabelIDs,
			Unread:  bool(msg.Unread),
		}
		if msg.Sender != nil {
			m.From = msg.Sender.Address
		}
		for _, addr := range msg.ToList {
			m.To = append(m.To, addr.Address)
		}
		for _, addr := range msg.CCList {
			m.CC = append(m.CC, addr.Address)
		}
		out = append(out, m)
	}

	outputJSON(out)
}

// --- move-message ---

type MoveResultOut struct {
	MessageID string `json:"message_id"`
	FromLabel string `json:"from_label"`
	ToLabel   string `json:"to_label"`
	Status    string `json:"status"`
}

func cmdMoveMessage(ctx context.Context, c *proton.Client) {
	args := os.Args[2:]
	messageID := requireFlag(args, "--message-id")
	folderID := requireFlag(args, "--folder-id")

	// Remove from Inbox, add to target folder.
	if err := c.UnlabelMessages(ctx, []string{messageID}, proton.InboxLabel); err != nil {
		log.Fatalf("UnlabelMessages: %v", err)
	}
	if err := c.LabelMessages(ctx, []string{messageID}, folderID); err != nil {
		log.Fatalf("LabelMessages: %v", err)
	}

	outputJSON(MoveResultOut{
		MessageID: messageID,
		FromLabel: proton.InboxLabel,
		ToLabel:   folderID,
		Status:    "moved",
	})
}

// --- create-folder ---

type CreateFolderOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func cmdCreateFolder(ctx context.Context, c *proton.Client) {
	args := os.Args[2:]
	name := requireFlag(args, "--name")
	parentID := optionalFlag(args, "--parent-id", "")

	label, err := c.CreateLabel(ctx, proton.CreateLabelReq{
		Name:     name,
		Color:    "#7272a7",
		Type:     proton.LabelTypeFolder,
		ParentID: parentID,
	})
	if err != nil {
		log.Fatalf("CreateLabel: %v", err)
	}

	outputJSON(CreateFolderOut{
		ID:   label.ID,
		Name: label.Name,
	})
}
