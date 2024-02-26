package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	graphql "github.com/machinebox/graphql"
	gitlab "github.com/xanzy/go-gitlab"
)

type TimelogData struct {
	Project struct {
		Issues struct {
			Nodes []struct {
				IID      string `json:"iid"`
				Title    string `json:"title"`
				Timelogs struct {
					Nodes []struct {
						TimeSpent int    `json:"timeSpent"`
						SpentAt   string `json:"spentAt"`
						User      struct {
							Username string `json:"username"`
						} `json:"user"`
					} `json:"nodes"`
				} `json:"timelogs"`
			} `json:"nodes"`
		} `json:"issues"`
	} `json:"project"`
}

func getTimelogs(projectId string, apiToken string, client *graphql.Client, ctx context.Context) (*TimelogData, error) {
	// Construct the GraphQL query
	req := graphql.NewRequest(`
		query($fullPath: ID!) {
			project(fullPath: $fullPath) {
				issues {
					nodes {
						iid
						title
						timelogs {
							nodes {
								timeSpent
								spentAt
								user {
									username
								}
							}
						}
					}
				}
			}
		}
		`)

	req.Var("fullPath", projectId)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))

	var data TimelogData
	if err := client.Run(ctx, req, &data); err != nil {
		return nil, err
	}

	return &data, nil
}

func getUserSpentTime(daysNum int, username string, timelogData *TimelogData) {

	var totalSpentTime float32
	date := time.Now().AddDate(0, 0, -daysNum).Format("2006-01-02")
	local, _ := time.LoadLocation("Local")

	for _, issue := range timelogData.Project.Issues.Nodes {
		for _, timelog := range issue.Timelogs.Nodes {

			// When selecting dates only, Gitlab will set the time to midnight local time
			// So it might fail to load timelogs for today as it can be minus few hours and lose one day (depending on the timezone)
			spentAt, _ := time.Parse(time.RFC3339, timelog.SpentAt)
			localSpentAt := spentAt.In(local).Format("2006-01-02")

			if localSpentAt >= date && timelog.User.Username == username {
				totalSpentTime += float32(timelog.TimeSpent) / 3600
				log.Printf("%.1fh at %s - #%s: %s\n", float32(timelog.TimeSpent)/3600, localSpentAt, issue.IID, issue.Title)
			}
		}
	}
	log.Printf("Total spent time since %s for %s : %.1fh", date, username, totalSpentTime)
}

func getAllUsersSpentTime(daysNum int, trackingIssue string, timelogData *TimelogData) {
	// store a map of username = total spent time on tickets
	totalDevTimePerUser := make(map[string]float32)
	totalNonDevTimePerUser := make(map[string]float32)

	date := time.Now().AddDate(0, 0, -daysNum).Format("2006-01-02")
	local, _ := time.LoadLocation("Local")

	for _, issue := range timelogData.Project.Issues.Nodes {
		for _, timelog := range issue.Timelogs.Nodes {

			// When selecting dates only, Gitlab will set the time to midnight local time
			// So it might fail to load timelogs for today as it can be minus few hours and lose one day (depending on the timezone)
			spentAt, _ := time.Parse(time.RFC3339, timelog.SpentAt)
			localSpentAt := spentAt.In(local).Format("2006-01-02")

			if localSpentAt >= date {
				if strings.Contains(issue.Title, trackingIssue) {
					totalNonDevTimePerUser[timelog.User.Username] += float32(timelog.TimeSpent) / 3600
				} else {
					totalDevTimePerUser[timelog.User.Username] += float32(timelog.TimeSpent) / 3600
				}
				log.Printf("%.1fh at %s by %s - #%s: %s\n", float32(timelog.TimeSpent)/3600, localSpentAt, timelog.User.Username, issue.IID, issue.Title)
			}
		}
	}

	log.Println("-- Total dev time spent --")

	var totalDevSpentTime float32
	for username, time := range totalDevTimePerUser {
		log.Printf("since %s for %s : %.1fh", date, username, time)
		totalDevSpentTime += time
	}

	log.Printf("Total : %.1fh", totalDevSpentTime)

	log.Println("-- Total NON dev time spent--")
	var totalNonDevSpentTime float32
	for username, time := range totalNonDevTimePerUser {
		log.Printf("since %s for %s : %.1fh", date, username, time)
		totalNonDevSpentTime += time
	}

	log.Printf("Total : %.1fh", totalNonDevSpentTime)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Could not load .env file, error: %s", err)
	}

	// Check env vars
	apiToken := os.Getenv("GITLAB_TOKEN")
	if apiToken == "" {
		log.Fatal("GITLAB_TOKEN environment variable is not set")
	}

	projectId := os.Getenv("GITLAB_PROJECT_PATH")
	if projectId == "" {
		log.Fatal("GITLAB_PROJECT_PATH environment variable is not set")
	}

	gitlabHost := os.Getenv("GITLAB_HOST")
	if gitlabHost == "" {
		gitlabHost = "https://gitlab.com"
		log.Printf("GITLAB_HOST is not set, using default %s", gitlabHost)
	}

	daysEnv := os.Getenv("DAYS_NUM")
	if daysEnv == "" {
		daysEnv = "0"
		log.Printf("DAYS_NUM is not set, using default %s", daysEnv)
	}

	daysNum, err := strconv.Atoi(daysEnv)
	if err != nil {
		log.Fatal("DAYS_NUM must be in integer, it represents the number of previous days to fetch issues for")
	}

	getAllUsers := os.Getenv("ALL_USERS")
	reportingIssue := os.Getenv("GITLAB_REPORTING_ISSUE")

	gitlabAPIUrl := gitlabHost + "/api/v4"
	gitlabGraphQLUrl := gitlabHost + "/api/graphql"

	// Get current username with the personal access token
	gitlabClient, err := gitlab.NewClient(apiToken, gitlab.WithBaseURL(gitlabAPIUrl))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	currentUser, _, err := gitlabClient.Users.CurrentUser()
	if err != nil {
		log.Fatalf("Failed to get current user: %v", err)
	}

	// Gitlab REST API does not provide timelog object on issues with who log what, only the graphQL API does that
	graphQLClient := graphql.NewClient(gitlabGraphQLUrl)

	// Get go context
	ctx := context.Background()

	timelogData, err := getTimelogs(projectId, apiToken, graphQLClient, ctx)
	if err != nil {
		log.Fatalf("Failed to execute query: %v", err)
	}

	if getAllUsers == "" {
		getUserSpentTime(daysNum, currentUser.Username, timelogData)
	} else {
		getAllUsersSpentTime(daysNum, reportingIssue, timelogData)
	}
}
