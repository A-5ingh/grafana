package notifiers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
	"github.com/grafana/grafana/pkg/services/alerting/notifiers"
	encryptionservice "github.com/grafana/grafana/pkg/services/encryption/service"
	"github.com/grafana/grafana/pkg/services/notifications"
	"github.com/grafana/grafana/pkg/services/org"
	"github.com/grafana/grafana/pkg/services/org/orgtest"
	"github.com/grafana/grafana/pkg/services/sqlstore"

	"github.com/stretchr/testify/require"
)

var (
	correctProperties            = "./testdata/test-configs/correct-properties"
	incorrectSettings            = "./testdata/test-configs/incorrect-settings"
	noRequiredFields             = "./testdata/test-configs/no-required-fields"
	correctPropertiesWithOrgName = "./testdata/test-configs/correct-properties-with-orgName"
	brokenYaml                   = "./testdata/test-configs/broken-yaml"
	doubleNotificationsConfig    = "./testdata/test-configs/double-default"
	emptyFolder                  = "./testdata/test-configs/empty_folder"
	emptyFile                    = "./testdata/test-configs/empty"
	twoNotificationsConfig       = "./testdata/test-configs/two-notifications"
	unknownNotifier              = "./testdata/test-configs/unknown-notifier"
)

func TestNotificationAsConfig(t *testing.T) {
	var sqlStore *sqlstore.SQLStore
	var ns *alerting.AlertNotificationService
	logger := log.New("fake.log")
	orgService := orgtest.NewOrgServiceFake()
	orgService.ExpectedOrg = &org.Org{}

	encryptionService := encryptionservice.SetupTestService(t)

	t.Run("Testing notification as configuration", func(t *testing.T) {
		setup := func() {
			sqlStore = sqlstore.InitTestDB(t)
			nm := &notifications.NotificationService{}
			ns = alerting.ProvideService(sqlStore, encryptionService, nm)

			for i := 1; i < 5; i++ {
				orgCommand := models.CreateOrgCommand{Name: fmt.Sprintf("Main Org. %v", i)}
				err := sqlStore.CreateOrg(context.Background(), &orgCommand)
				require.NoError(t, err)
			}

			alerting.RegisterNotifier(&alerting.NotifierPlugin{
				Type:    "slack",
				Name:    "slack",
				Factory: notifiers.NewSlackNotifier,
			})

			alerting.RegisterNotifier(&alerting.NotifierPlugin{
				Type:    "email",
				Name:    "email",
				Factory: notifiers.NewEmailNotifier,
			})
		}

		t.Run("Can read correct properties", func(t *testing.T) {
			setup()
			_ = os.Setenv("TEST_VAR", "default")
			cfgProvider := &configReader{
				orgStore:          sqlStore,
				encryptionService: encryptionService,
				log:               log.New("test logger"),
			}

			cfg, err := cfgProvider.readConfig(context.Background(), correctProperties)
			_ = os.Unsetenv("TEST_VAR")
			if err != nil {
				t.Fatalf("readConfig return an error %v", err)
			}
			require.Equal(t, len(cfg), 1)

			ntCfg := cfg[0]
			nts := ntCfg.Notifications
			require.Equal(t, len(nts), 4)

			nt := nts[0]
			require.Equal(t, nt.Name, "default-slack-notification")
			require.Equal(t, nt.Type, "slack")
			require.Equal(t, nt.OrgID, int64(2))
			require.Equal(t, nt.UID, "notifier1")
			require.True(t, nt.IsDefault)
			require.Equal(t, nt.Settings, map[string]interface{}{
				"recipient": "XXX", "token": "xoxb", "uploadImage": true, "url": "https://slack.com",
			})
			require.Equal(t, nt.SecureSettings, map[string]string{
				"token": "xoxbsecure", "url": "https://slack.com/secure",
			})
			require.True(t, nt.SendReminder)
			require.Equal(t, nt.Frequency, "1h")

			nt = nts[1]
			require.Equal(t, nt.Name, "another-not-default-notification")
			require.Equal(t, nt.Type, "email")
			require.Equal(t, nt.OrgID, int64(3))
			require.Equal(t, nt.UID, "notifier2")
			require.False(t, nt.IsDefault)

			nt = nts[2]
			require.Equal(t, nt.Name, "check-unset-is_default-is-false")
			require.Equal(t, nt.Type, "slack")
			require.Equal(t, nt.OrgID, int64(3))
			require.Equal(t, nt.UID, "notifier3")
			require.False(t, nt.IsDefault)

			nt = nts[3]
			require.Equal(t, nt.Name, "Added notification with whitespaces in name")
			require.Equal(t, nt.Type, "email")
			require.Equal(t, nt.UID, "notifier4")
			require.Equal(t, nt.OrgID, int64(3))

			deleteNts := ntCfg.DeleteNotifications
			require.Equal(t, len(deleteNts), 4)

			deleteNt := deleteNts[0]
			require.Equal(t, deleteNt.Name, "default-slack-notification")
			require.Equal(t, deleteNt.UID, "notifier1")
			require.Equal(t, deleteNt.OrgID, int64(2))

			deleteNt = deleteNts[1]
			require.Equal(t, deleteNt.Name, "deleted-notification-without-orgId")
			require.Equal(t, deleteNt.OrgID, int64(1))
			require.Equal(t, deleteNt.UID, "notifier2")

			deleteNt = deleteNts[2]
			require.Equal(t, deleteNt.Name, "deleted-notification-with-0-orgId")
			require.Equal(t, deleteNt.OrgID, int64(1))
			require.Equal(t, deleteNt.UID, "notifier3")

			deleteNt = deleteNts[3]
			require.Equal(t, deleteNt.Name, "Deleted notification with whitespaces in name")
			require.Equal(t, deleteNt.OrgID, int64(1))
			require.Equal(t, deleteNt.UID, "notifier4")
		})

		t.Run("One configured notification", func(t *testing.T) {
			t.Run("no notification in database", func(t *testing.T) {
				setup()
				fakeAlertNotification := &fakeAlertNotification{}
				fakeAlertNotification.ExpectedAlertNotification = &models.AlertNotification{OrgId: 1}
				dc := newNotificationProvisioner(orgService, sqlStore, fakeAlertNotification, encryptionService, nil, logger)

				err := dc.applyChanges(context.Background(), twoNotificationsConfig)
				if err != nil {
					t.Fatalf("applyChanges return an error %v", err)
				}
			})

			t.Run("One notification in database with same name and uid", func(t *testing.T) {
				setup()
				existingNotificationCmd := models.CreateAlertNotificationCommand{
					Name:  "channel1",
					OrgId: 1,
					Uid:   "notifier1",
					Type:  "slack",
				}
				err := ns.SQLStore.CreateAlertNotificationCommand(context.Background(), &existingNotificationCmd)
				require.NoError(t, err)
				require.NotNil(t, existingNotificationCmd.Result)
				notificationsQuery := models.GetAllAlertNotificationsQuery{OrgId: 1}
				err = ns.SQLStore.GetAllAlertNotifications(context.Background(), &notificationsQuery)
				require.NoError(t, err)
				require.NotNil(t, notificationsQuery.Result)
				require.Equal(t, len(notificationsQuery.Result), 1)

				t.Run("should update one notification", func(t *testing.T) {
					dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
					err = dc.applyChanges(context.Background(), twoNotificationsConfig)
					if err != nil {
						t.Fatalf("applyChanges return an error %v", err)
					}
				})
			})

			t.Run("Two notifications with is_default", func(t *testing.T) {
				setup()
				dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
				err := dc.applyChanges(context.Background(), doubleNotificationsConfig)
				t.Run("should both be inserted", func(t *testing.T) {
					require.NoError(t, err)
				})
			})
		})

		t.Run("Two configured notification", func(t *testing.T) {
			t.Run("two other notifications in database", func(t *testing.T) {
				setup()
				existingNotificationCmd := models.CreateAlertNotificationCommand{
					Name:  "channel0",
					OrgId: 1,
					Uid:   "notifier0",
					Type:  "slack",
				}
				err := ns.SQLStore.CreateAlertNotificationCommand(context.Background(), &existingNotificationCmd)
				require.NoError(t, err)
				existingNotificationCmd = models.CreateAlertNotificationCommand{
					Name:  "channel3",
					OrgId: 1,
					Uid:   "notifier3",
					Type:  "slack",
				}
				err = ns.SQLStore.CreateAlertNotificationCommand(context.Background(), &existingNotificationCmd)
				require.NoError(t, err)

				notificationsQuery := models.GetAllAlertNotificationsQuery{OrgId: 1}
				err = ns.GetAllAlertNotifications(context.Background(), &notificationsQuery)
				require.NoError(t, err)
				require.NotNil(t, notificationsQuery.Result)
				require.Equal(t, len(notificationsQuery.Result), 2)

				t.Run("should have two new notifications", func(t *testing.T) {
					dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
					err := dc.applyChanges(context.Background(), twoNotificationsConfig)
					if err != nil {
						t.Fatalf("applyChanges return an error %v", err)
					}
				})
			})
		})

		t.Run("Can read correct properties with orgName instead of orgId", func(t *testing.T) {
			setup()

			existingOrg1 := models.GetOrgByNameQuery{Name: "Main Org. 1"}
			err := sqlStore.GetOrgByNameHandler(context.Background(), &existingOrg1)
			require.NoError(t, err)
			require.NotNil(t, existingOrg1.Result)
			existingOrg2 := models.GetOrgByNameQuery{Name: "Main Org. 2"}
			err = sqlStore.GetOrgByNameHandler(context.Background(), &existingOrg2)
			require.NoError(t, err)
			require.NotNil(t, existingOrg2.Result)

			existingNotificationCmd := models.CreateAlertNotificationCommand{
				Name:  "default-notification-delete",
				OrgId: existingOrg2.Result.Id,
				Uid:   "notifier2",
				Type:  "slack",
			}
			err = ns.SQLStore.CreateAlertNotificationCommand(context.Background(), &existingNotificationCmd)
			require.NoError(t, err)

			dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
			err = dc.applyChanges(context.Background(), correctPropertiesWithOrgName)
			if err != nil {
				t.Fatalf("applyChanges return an error %v", err)
			}
		})

		t.Run("Config doesn't contain required field", func(t *testing.T) {
			setup()
			dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
			err := dc.applyChanges(context.Background(), noRequiredFields)
			require.NotNil(t, err)

			errString := err.Error()
			require.Contains(t, errString, "Deleted alert notification item 1 in configuration doesn't contain required field uid")
			require.Contains(t, errString, "Deleted alert notification item 2 in configuration doesn't contain required field name")
			require.Contains(t, errString, "Added alert notification item 1 in configuration doesn't contain required field name")
			require.Contains(t, errString, "Added alert notification item 2 in configuration doesn't contain required field uid")
		})

		t.Run("Empty yaml file", func(t *testing.T) {
			t.Run("should have not changed repo", func(t *testing.T) {
				setup()
				dc := newNotificationProvisioner(orgService, sqlStore, &fakeAlertNotification{}, encryptionService, nil, logger)
				err := dc.applyChanges(context.Background(), emptyFile)
				if err != nil {
					t.Fatalf("applyChanges return an error %v", err)
				}
				notificationsQuery := models.GetAllAlertNotificationsQuery{OrgId: 1}
				err = ns.GetAllAlertNotifications(context.Background(), &notificationsQuery)
				require.NoError(t, err)
				require.Empty(t, notificationsQuery.Result)
			})
		})

		t.Run("Broken yaml should return error", func(t *testing.T) {
			reader := &configReader{
				orgStore:          sqlStore,
				encryptionService: encryptionService,
				log:               log.New("test logger"),
			}

			_, err := reader.readConfig(context.Background(), brokenYaml)
			require.NotNil(t, err)
		})

		t.Run("Skip invalid directory", func(t *testing.T) {
			cfgProvider := &configReader{
				orgStore:          sqlStore,
				encryptionService: encryptionService,
				log:               log.New("test logger"),
			}

			cfg, err := cfgProvider.readConfig(context.Background(), emptyFolder)
			if err != nil {
				t.Fatalf("readConfig return an error %v", err)
			}
			require.Equal(t, len(cfg), 0)
		})

		t.Run("Unknown notifier should return error", func(t *testing.T) {
			cfgProvider := &configReader{
				orgStore:          sqlStore,
				encryptionService: encryptionService,
				log:               log.New("test logger"),
			}
			_, err := cfgProvider.readConfig(context.Background(), unknownNotifier)
			require.NotNil(t, err)
			require.Equal(t, err.Error(), `unsupported notification type "nonexisting"`)
		})

		t.Run("Read incorrect properties", func(t *testing.T) {
			cfgProvider := &configReader{
				orgStore:          sqlStore,
				encryptionService: encryptionService,
				log:               log.New("test logger"),
			}
			_, err := cfgProvider.readConfig(context.Background(), incorrectSettings)
			require.NotNil(t, err)
			require.Equal(t, err.Error(), "alert validation error: token must be specified when using the Slack chat API")
		})
	})
}

type fakeAlertNotification struct {
	ExpectedAlertNotification *models.AlertNotification
}

func (f *fakeAlertNotification) GetAlertNotifications(ctx context.Context, query *models.GetAlertNotificationsQuery) error {
	query.Result = f.ExpectedAlertNotification
	return nil
}
func (f *fakeAlertNotification) CreateAlertNotificationCommand(ctx context.Context, cmd *models.CreateAlertNotificationCommand) error {
	return nil
}
func (f *fakeAlertNotification) UpdateAlertNotification(ctx context.Context, cmd *models.UpdateAlertNotificationCommand) error {
	return nil
}
func (f *fakeAlertNotification) DeleteAlertNotification(ctx context.Context, cmd *models.DeleteAlertNotificationCommand) error {
	return nil
}
func (f *fakeAlertNotification) GetAllAlertNotifications(ctx context.Context, query *models.GetAllAlertNotificationsQuery) error {
	return nil
}
func (f *fakeAlertNotification) GetOrCreateAlertNotificationState(ctx context.Context, cmd *models.GetOrCreateNotificationStateQuery) error {
	return nil
}
func (f *fakeAlertNotification) SetAlertNotificationStateToCompleteCommand(ctx context.Context, cmd *models.SetAlertNotificationStateToCompleteCommand) error {
	return nil
}
func (f *fakeAlertNotification) SetAlertNotificationStateToPendingCommand(ctx context.Context, cmd *models.SetAlertNotificationStateToPendingCommand) error {
	return nil
}
func (f *fakeAlertNotification) GetAlertNotificationsWithUid(ctx context.Context, query *models.GetAlertNotificationsWithUidQuery) error {
	return nil
}
func (f *fakeAlertNotification) DeleteAlertNotificationWithUid(ctx context.Context, cmd *models.DeleteAlertNotificationWithUidCommand) error {
	return nil
}
func (f *fakeAlertNotification) GetAlertNotificationsWithUidToSend(ctx context.Context, query *models.GetAlertNotificationsWithUidToSendQuery) error {
	return nil
}

func (f *fakeAlertNotification) UpdateAlertNotificationWithUid(ctx context.Context, cmd *models.UpdateAlertNotificationWithUidCommand) error {
	return nil
}
