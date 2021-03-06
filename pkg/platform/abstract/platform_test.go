package abstract

import (
	"bufio"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/nuclio/nuclio/pkg/common"
	"github.com/nuclio/nuclio/pkg/containerimagebuilderpusher"
	"github.com/nuclio/nuclio/pkg/dockerclient"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/platform/mock"
	"github.com/nuclio/nuclio/pkg/platformconfig"

	"github.com/nuclio/logger"
	"github.com/nuclio/zap"
	"github.com/rs/xid"
	"github.com/stretchr/testify/suite"
)

const (
	MultiWorkerFunctionLogsFilePath          = "test/logs_examples/multi_worker"
	PanicFunctionLogsFilePath                = "test/logs_examples/panic"
	GoWithCallStackFunctionLogsFilePath      = "test/logs_examples/go_with_call_stack"
	SpecialSubstringsFunctionLogsFilePath    = "test/logs_examples/special_substrings"
	ConsecutiveDuplicateFunctionLogsFilePath = "test/logs_examples/consecutive_duplicate"
	FunctionLogsFile                         = "function_logs.txt"
	FormattedFunctionLogsFile                = "formatted_function_logs.txt"
	BriefErrorsMessageFile                   = "brief_errors_message.txt"
)

type AbstractPlatformTestSuite struct {
	suite.Suite
	mockedPlatform *mock.Platform

	Logger           logger.Logger
	DockerClient     dockerclient.Client
	Platform         *Platform
	TestID           string
	Runtime          string
	RuntimeDir       string
	FunctionDir      string
	TempDir          string
	CleanupTemp      bool
	DefaultNamespace string
}

func (suite *AbstractPlatformTestSuite) SetupSuite() {
	var err error

	common.SetVersionFromEnv()

	suite.DefaultNamespace = "nuclio"

	suite.Logger, err = nucliozap.NewNuclioZapTest("test")
	suite.Require().NoError(err, "Logger should create successfully")

	suite.mockedPlatform = &mock.Platform{}
	suite.Platform, err = NewPlatform(suite.Logger, suite.mockedPlatform, &platformconfig.Config{})
	suite.Require().NoError(err, "Could not create platform")

	suite.Platform.ContainerBuilder, err = containerimagebuilderpusher.NewNop(suite.Logger, nil)
	suite.Require().NoError(err)
}

func (suite *AbstractPlatformTestSuite) SetupTest() {
	suite.TestID = xid.New().String()
}

func (suite *AbstractPlatformTestSuite) TestValidateDeleteFunctionOptions() {
	for _, testCase := range []struct {
		existingFunctions     []platform.Function
		deleteFunctionOptions *platform.DeleteFunctionOptions
		shouldFailValidation  bool
	}{

		// happy flow
		{
			existingFunctions: []platform.Function{
				&platform.AbstractFunction{
					Logger:   suite.Logger,
					Platform: suite.Platform.platform,
					Config: functionconfig.Config{
						Meta: functionconfig.Meta{
							Name: "existing",
						},
					},
				},
			},
			deleteFunctionOptions: &platform.DeleteFunctionOptions{
				FunctionConfig: functionconfig.Config{
					Meta: functionconfig.Meta{
						Name: "existing",
					},
				},
			},
		},

		// function may not be existing, validation should pass (delete is idempotent)
		{
			deleteFunctionOptions: &platform.DeleteFunctionOptions{
				FunctionConfig: functionconfig.Config{
					Meta: functionconfig.Meta{
						Name:            "not-existing",
						ResourceVersion: "",
					},
					Spec: functionconfig.Spec{},
				},
			},
		},

		// matching resourceVersion
		{
			existingFunctions: []platform.Function{
				&platform.AbstractFunction{
					Logger:   suite.Logger,
					Platform: suite.Platform.platform,
					Config: functionconfig.Config{
						Meta: functionconfig.Meta{
							Name:            "existing",
							ResourceVersion: "1",
						},
					},
				},
			},
			deleteFunctionOptions: &platform.DeleteFunctionOptions{
				FunctionConfig: functionconfig.Config{
					Meta: functionconfig.Meta{
						Name:            "existing",
						ResourceVersion: "1",
					},
				},
			},
		},

		// fail: stale resourceVersion
		{
			existingFunctions: []platform.Function{
				&platform.AbstractFunction{
					Logger:   suite.Logger,
					Platform: suite.Platform.platform,
					Config: functionconfig.Config{
						Meta: functionconfig.Meta{
							Name:            "existing",
							ResourceVersion: "2",
						},
					},
				},
			},
			deleteFunctionOptions: &platform.DeleteFunctionOptions{
				FunctionConfig: functionconfig.Config{
					Meta: functionconfig.Meta{
						Name:            "existing",
						ResourceVersion: "1",
					},
				},
			},
			shouldFailValidation: true,
		},
	} {

		suite.mockedPlatform.On("GetFunctions", &platform.GetFunctionsOptions{
			Name:      testCase.deleteFunctionOptions.FunctionConfig.Meta.Name,
			Namespace: testCase.deleteFunctionOptions.FunctionConfig.Meta.Namespace,
		}).Return(testCase.existingFunctions, nil).Once()

		err := suite.Platform.ValidateDeleteFunctionOptions(testCase.deleteFunctionOptions)
		if testCase.shouldFailValidation {
			suite.Require().Error(err)
		} else {
			suite.Require().NoError(err)
		}
	}
}

// Test function with invalid min max replicas
func (suite *AbstractPlatformTestSuite) TestMinMaxReplicas() {
	zero := 0
	one := 1
	two := 2
	for idx, MinMaxReplicas := range []struct {
		MinReplicas          *int
		MaxReplicas          *int
		ExpectedMinReplicas  *int
		ExpectedMaxReplicas  *int
		shouldFailValidation bool
	}{
		{MinReplicas: nil, MaxReplicas: nil, ExpectedMinReplicas: nil, ExpectedMaxReplicas: nil, shouldFailValidation: false},
		{MinReplicas: nil, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: nil, MaxReplicas: &one, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: nil, MaxReplicas: &two, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &zero, MaxReplicas: nil, shouldFailValidation: true},
		{MinReplicas: &zero, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &zero, MaxReplicas: &one, ExpectedMinReplicas: &zero, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &zero, MaxReplicas: &two, ExpectedMinReplicas: &zero, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &one, MaxReplicas: nil, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &one, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &one, MaxReplicas: &one, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &one, MaxReplicas: &two, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &two, MaxReplicas: nil, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},
		{MinReplicas: &two, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &two, MaxReplicas: &one, shouldFailValidation: true},
		{MinReplicas: &two, MaxReplicas: &two, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},
	} {

		suite.mockedPlatform.On("GetProjects", &platform.GetProjectsOptions{
			Meta: platform.ProjectMeta{
				Name:      platform.DefaultProjectName,
				Namespace: "default",
			},
		}).Return([]platform.Project{
			&platform.AbstractProject{},
		}, nil).Once()

		// name it with index and shift with 65 to get A as first letter
		functionName := string(rune(idx + 65))
		functionConfig := *functionconfig.NewConfig()

		createFunctionOptions := &platform.CreateFunctionOptions{
			Logger:         suite.Logger,
			FunctionConfig: functionConfig,
		}

		createFunctionOptions.FunctionConfig.Meta.Name = functionName
		createFunctionOptions.FunctionConfig.Meta.Labels = map[string]string{
			"nuclio.io/project-name": platform.DefaultProjectName,
		}
		createFunctionOptions.FunctionConfig.Spec.MinReplicas = MinMaxReplicas.MinReplicas
		createFunctionOptions.FunctionConfig.Spec.MaxReplicas = MinMaxReplicas.MaxReplicas
		suite.Logger.DebugWith("Checking function ", "functionName", functionName)

		err := suite.Platform.EnrichCreateFunctionOptions(createFunctionOptions)
		suite.Require().NoError(err, "Failed to enrich function")

		err = suite.Platform.ValidateCreateFunctionOptions(createFunctionOptions)
		if MinMaxReplicas.shouldFailValidation {
			suite.Error(err, "Validation should fail")
			suite.Logger.DebugWith("Validation failed as expected ", "functionName", functionName)
			continue
		}
		suite.Require().NoError(err, "Failed to validate function")
		functionConfigSpec := createFunctionOptions.FunctionConfig.Spec

		if MinMaxReplicas.ExpectedMinReplicas != nil {
			suite.Assert().Equal(*MinMaxReplicas.ExpectedMinReplicas, *functionConfigSpec.MinReplicas)
		} else {
			suite.Assert().Nil(functionConfigSpec.MinReplicas)
		}

		if MinMaxReplicas.ExpectedMaxReplicas != nil {
			suite.Assert().Equal(*MinMaxReplicas.ExpectedMaxReplicas, *functionConfigSpec.MaxReplicas)
		} else {
			suite.Assert().Nil(functionConfigSpec.MaxReplicas)
		}
		suite.Logger.DebugWith("Validation passed successfully", "functionName", functionName)
	}
}

func (suite *AbstractPlatformTestSuite) TestEnrichAndValidateFunctionTriggers() {
	for idx, testCase := range []struct {
		triggers                 map[string]functionconfig.Trigger
		expectedEnrichedTriggers map[string]functionconfig.Trigger
		shouldFailValidation     bool
	}{

		// enrich maxWorkers to 1
		// enrich name from key
		{
			triggers: map[string]functionconfig.Trigger{
				"some-trigger": {
					Kind: "http",
				},
			},
			expectedEnrichedTriggers: map[string]functionconfig.Trigger{
				"some-trigger": {
					Kind:       "http",
					MaxWorkers: 1,
					Name:       "some-trigger",
				},
			},
		},

		// enrich with default http trigger
		{
			triggers: nil,
			expectedEnrichedTriggers: func() map[string]functionconfig.Trigger {
				defaultHTTPTrigger := functionconfig.GetDefaultHTTPTrigger()
				return map[string]functionconfig.Trigger{
					defaultHTTPTrigger.Name: defaultHTTPTrigger,
				}
			}(),
		},

		// do not allow more than 1 http trigger
		{
			triggers: map[string]functionconfig.Trigger{
				"firstHTTPTrigger": {
					Kind: "http",
				},
				"secondHTTPTrigger": {
					Kind: "http",
				},
			},
			shouldFailValidation: true,
		},

		// do not allow empty name triggers
		{
			triggers: map[string]functionconfig.Trigger{
				"": {
					Kind: "http",
				},
			},
			shouldFailValidation: true,
		},
	} {

		suite.mockedPlatform.On("GetProjects", &platform.GetProjectsOptions{
			Meta: platform.ProjectMeta{
				Name:      platform.DefaultProjectName,
				Namespace: "default",
			},
		}).Return([]platform.Project{
			&platform.AbstractProject{},
		}, nil).Once()

		// name it with index and shift with 65 to get A as first letter
		functionName := string(rune(idx + 65))
		functionConfig := *functionconfig.NewConfig()

		createFunctionOptions := &platform.CreateFunctionOptions{
			Logger:         suite.Logger,
			FunctionConfig: functionConfig,
		}
		createFunctionOptions.FunctionConfig.Meta.Name = functionName
		createFunctionOptions.FunctionConfig.Meta.Labels = map[string]string{
			"nuclio.io/project-name": platform.DefaultProjectName,
		}
		createFunctionOptions.FunctionConfig.Spec.Triggers = testCase.triggers
		suite.Logger.DebugWith("Checking function ", "functionName", functionName)

		err := suite.Platform.EnrichCreateFunctionOptions(createFunctionOptions)
		suite.Require().NoError(err, "Failed to enrich function")

		err = suite.Platform.ValidateCreateFunctionOptions(createFunctionOptions)
		if testCase.shouldFailValidation {
			suite.Require().Error(err, "Validation passed unexpectedly")
			continue
		}

		suite.Require().NoError(err, "Validation failed unexpectedly")
		suite.Equal(testCase.expectedEnrichedTriggers,
			createFunctionOptions.FunctionConfig.Spec.Triggers)
	}
}

func (suite *AbstractPlatformTestSuite) TestGetProcessorLogsOnMultiWorker() {
	suite.testGetProcessorLogsTestFromFile(MultiWorkerFunctionLogsFilePath)
}

func (suite *AbstractPlatformTestSuite) TestGetProcessorLogsOnPanic() {
	suite.testGetProcessorLogsTestFromFile(PanicFunctionLogsFilePath)
}

func (suite *AbstractPlatformTestSuite) TestGetProcessorLogsOnGoWithCallStack() {
	suite.testGetProcessorLogsTestFromFile(GoWithCallStackFunctionLogsFilePath)
}

func (suite *AbstractPlatformTestSuite) TestGetProcessorLogsWithSpecialSubstrings() {
	suite.testGetProcessorLogsTestFromFile(SpecialSubstringsFunctionLogsFilePath)
}

func (suite *AbstractPlatformTestSuite) TestGetProcessorLogsWithConsecutiveDuplicateMessages() {
	suite.testGetProcessorLogsTestFromFile(ConsecutiveDuplicateFunctionLogsFilePath)
}

// Test that GetProcessorLogs() generates the expected formattedPodLogs and briefErrorsMessage
// Expects 3 files inside functionLogsFilePath: (kept in these constants)
// - FunctionLogsFile
// - FormattedFunctionLogsFile
// - BriefErrorsMessageFile
func (suite *AbstractPlatformTestSuite) testGetProcessorLogsTestFromFile(functionLogsFilePath string) {
	functionLogsFile, err := os.Open(path.Join(functionLogsFilePath, FunctionLogsFile))
	suite.Require().NoError(err, "Failed to read function logs file")

	functionLogsScanner := bufio.NewScanner(functionLogsFile)

	formattedPodLogs, briefErrorsMessage := suite.Platform.GetProcessorLogsAndBriefError(functionLogsScanner)

	expectedFormattedFunctionLogsFileBytes, err := ioutil.ReadFile(path.Join(functionLogsFilePath, FormattedFunctionLogsFile))
	suite.Require().NoError(err, "Failed to read formatted function logs file")
	suite.Assert().Equal(string(expectedFormattedFunctionLogsFileBytes), formattedPodLogs)

	expectedBriefErrorsMessageFileBytes, err := ioutil.ReadFile(path.Join(functionLogsFilePath, BriefErrorsMessageFile))
	suite.Require().NoError(err, "Failed to read brief errors message file")
	suite.Assert().Equal(string(expectedBriefErrorsMessageFileBytes), briefErrorsMessage)
}

func TestAbstractPlatformTestSuite(t *testing.T) {
	suite.Run(t, new(AbstractPlatformTestSuite))
}
