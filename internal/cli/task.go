package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var (
	taskPriority    string
	taskDescription string
	taskAssign      string
	taskRole        string
	taskParent      int64
)

var taskCmd = &cobra.Command{
	Use:   "task [title]",
	Short: "Create or manage tasks",
	Long:  "Create a new task or manage existing ones on the board.",
}

var taskCreateCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new task",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTaskCreate,
}

var taskListCmd = &cobra.Command{
	Use:   "list [status]",
	Short: "List tasks, optionally filtered by status",
	RunE:  runTaskList,
}

var taskShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskShow,
}

var taskAssignCmd = &cobra.Command{
	Use:   "assign [id] [agent]",
	Short: "Assign an agent to a task",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskAssign,
}

var taskBlockCmd = &cobra.Command{
	Use:   "block [id] [reason]",
	Short: "Mark a task as blocked",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runTaskBlock,
}

var taskDoneCmd = &cobra.Command{
	Use:   "done [id]",
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDone,
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel [id]",
	Short: "Cancel a task — skip it in the pipeline",
	Long:  "Marks a task as cancelled. The pipeline will skip it. Epic can be accepted without it.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCancel,
}

func init() {
	taskCreateCmd.Flags().StringVarP(&taskPriority, "priority", "p", "medium", "Priority: high, medium, low")
	taskCreateCmd.Flags().StringVarP(&taskDescription, "desc", "d", "", "Task description")
	taskCreateCmd.Flags().Int64Var(&taskParent, "parent", 0, "Parent task ID")

	taskAssignCmd.Flags().StringVarP(&taskRole, "role", "r", "", "Role for the agent")

	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskShowCmd)
	taskCmd.AddCommand(taskAssignCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskCancelCmd)
}

func runTaskCreate(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	title := strings.Join(args, " ")
	var parentID *int64
	if taskParent > 0 {
		parentID = &taskParent
	}

	task, err := s.CreateTask(title, taskDescription, taskPriority, parentID)
	if err != nil {
		return err
	}

	fmt.Printf("Created task #%d: %s [%s]\n", task.ID, task.Title, task.Priority)
	return nil
}

func runTaskList(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	status := ""
	if len(args) > 0 {
		status = args[0]
	}

	tasks, err := s.ListTasks(status)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	for _, t := range tasks {
		agent := ""
		if t.AssignedAgent != "" {
			agent = fmt.Sprintf(" [%s]", t.AssignedAgent)
		}
		blocked := ""
		if t.Status == store.StatusBlocked {
			blocked = fmt.Sprintf(" BLOCKED: %q", t.BlockedReason)
		}
		fmt.Printf("#%-4d %-12s %-6s %s%s%s\n", t.ID, t.Status, t.Priority, t.Title, agent, blocked)
	}
	return nil
}

func runTaskShow(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	task, err := s.GetTask(id)
	if err != nil {
		return err
	}

	label := "Task"
	if task.Kind == store.KindEpic {
		label = "Epic"
	}
	fmt.Printf("%s #%d\n", label, task.ID)
	fmt.Printf("  Title:    %s\n", task.Title)
	fmt.Printf("  Kind:     %s\n", task.Kind)
	fmt.Printf("  Status:   %s\n", task.Status)
	fmt.Printf("  Priority: %s\n", task.Priority)
	if task.Description != "" {
		fmt.Printf("  Desc:     %s\n", task.Description)
	}
	if task.AssignedAgent != "" {
		fmt.Printf("  Agent:    %s (%s)\n", task.AssignedAgent, task.Role)
	}
	if task.BlockedReason != "" {
		fmt.Printf("  Blocked:  %s\n", task.BlockedReason)
	}
	if task.ParentID != nil {
		fmt.Printf("  Epic:     #%d\n", *task.ParentID)
	}
	if task.GitBranch != "" {
		fmt.Printf("  Branch:   %s\n", task.GitBranch)
	}
	fmt.Printf("  Created:  %s\n", task.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Updated:  %s\n", task.UpdatedAt.Format("2006-01-02 15:04"))

	// Show events.
	events, err := s.GetEvents(id)
	if err != nil {
		return err
	}
	if len(events) > 0 {
		fmt.Println("\n  Events:")
		for _, e := range events {
			agent := ""
			if e.Agent != "" {
				agent = fmt.Sprintf("[%s] ", e.Agent)
			}
			fmt.Printf("    %s %s%s: %s\n", e.Timestamp.Format("15:04"), agent, e.Type, e.Content)
		}
	}

	return nil
}

func runTaskAssign(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	agent := args[1]
	if err := s.AssignTask(id, agent, taskRole); err != nil {
		return err
	}

	fmt.Printf("Assigned task #%d to %s\n", id, agent)
	return nil
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	reason := strings.Join(args[1:], " ")
	if err := s.BlockTask(id, reason); err != nil {
		return err
	}

	fmt.Printf("Task #%d blocked: %s\n", id, reason)
	return nil
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	if err := s.UpdateTaskStatus(id, store.StatusDone); err != nil {
		return err
	}

	fmt.Printf("Task #%d marked as done\n", id)
	return nil
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("task #%d not found", id)
	}

	if task.Status == store.StatusDone {
		return fmt.Errorf("task #%d is already done", id)
	}

	if err := s.UpdateTaskStatus(id, store.StatusCancelled); err != nil {
		return err
	}
	s.AddEvent(id, "user", "cancelled", "Task cancelled by user")

	fmt.Printf("Cancelled task #%d: %s\n", id, task.Title)
	fmt.Printf("  Pipeline will skip this task. Epic can be accepted without it.\n")
	return nil
}
