// Copyright © 2022 Ettore Di Giacinto <mudler@c3os.io>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package config_test

import (
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mocks "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"path/filepath"
	"reflect"
	"strings"

	. "github.com/kairos-io/kairos-agent/v2/pkg/config"
	. "github.com/kairos-io/kairos-sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getTagName(s string) string {
	if len(s) < 1 {
		return ""
	}

	if s == "-" {
		return ""
	}

	f := func(c rune) bool {
		return c == '"' || c == ','
	}
	index := strings.IndexFunc(s, f)
	if index == -1 {
		return s
	}

	return s[:index]
}

func structContainsField(f, t string, str interface{}) bool {
	values := reflect.ValueOf(str)
	types := values.Type()

	for j := 0; j < values.NumField(); j++ {
		tagName := getTagName(types.Field(j).Tag.Get("json"))
		if types.Field(j).Name == f || tagName == t {
			return true
		} else {
			if types.Field(j).Type.Kind() == reflect.Struct {
				if types.Field(j).Type.Name() != "" {
					model := reflect.New(types.Field(j).Type)
					if instance, ok := model.Interface().(OneOfModel); ok {
						for _, childSchema := range instance.JSONSchemaOneOf() {
							if structContainsField(f, t, childSchema) {
								return true
							}
						}
					}
				}
			}
		}
	}

	return false
}

func structFieldsContainedInOtherStruct(left, right interface{}) {
	leftValues := reflect.ValueOf(left)
	leftTypes := leftValues.Type()

	for i := 0; i < leftValues.NumField(); i++ {
		leftTagName := getTagName(leftTypes.Field(i).Tag.Get("yaml"))
		leftFieldName := leftTypes.Field(i).Name
		if leftTypes.Field(i).IsExported() {
			It(fmt.Sprintf("Checks that the new schema contians the field %s", leftFieldName), func() {
				Expect(
					structContainsField(leftFieldName, leftTagName, right),
				).To(BeTrue())
			})
		}
	}
}

var _ = Describe("Schema", func() {
	Context("NewConfigFromYAML", func() {
		Context("While the new Schema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Config{}, RootSchema{})
		})
		Context("While the new InstallSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Install{}, InstallSchema{})
		})
		Context("While the new BundleSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Bundle{}, BundleSchema{})
		})
	})
	Describe("Write and load installation state", func() {
		var config *Config
		var runner *v1mocks.FakeRunner
		var fs vfs.FS
		var mounter *v1mocks.ErrorMounter
		var cleanup func()
		var err error
		var dockerState, channelState *v1.ImageState
		var installState *v1.InstallState
		var statePath, recoveryPath string

		BeforeEach(func() {
			runner = v1mocks.NewFakeRunner()
			mounter = v1mocks.NewErrorMounter()
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
			Expect(err).Should(BeNil())

			config = NewConfig(
				WithFs(fs),
				WithRunner(runner),
				WithMounter(mounter),
			)
			dockerState = &v1.ImageState{
				Source: v1.NewDockerSrc("registry.org/my/image:tag"),
				Label:  "active_label",
				FS:     "ext2",
				SourceMetadata: &v1.DockerImageMeta{
					Digest: "adadgadg",
					Size:   23452345,
				},
			}
			installState = &v1.InstallState{
				Date: "somedate",
				Partitions: map[string]*v1.PartitionState{
					"state": {
						FSLabel: "state_label",
						Images: map[string]*v1.ImageState{
							"active": dockerState,
						},
					},
					"recovery": {
						FSLabel: "state_label",
						Images: map[string]*v1.ImageState{
							"recovery": channelState,
						},
					},
				},
			}

			statePath = filepath.Join(constants.RunningStateDir, constants.InstallStateFile)
			recoveryPath = "/recoverypart/state.yaml"
			err = fsutils.MkdirAll(fs, filepath.Dir(recoveryPath), constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = fsutils.MkdirAll(fs, filepath.Dir(statePath), constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
		})
		AfterEach(func() {
			cleanup()
		})
		It("Writes and loads an installation data", func() {
			err = config.WriteInstallState(installState, statePath, recoveryPath)
			Expect(err).ShouldNot(HaveOccurred())
			loadedInstallState, err := config.LoadInstallState()
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*loadedInstallState).To(Equal(*installState))
		})
		It("Fails writing to state partition", func() {
			err = fs.RemoveAll(filepath.Dir(statePath))
			Expect(err).ShouldNot(HaveOccurred())
			err = config.WriteInstallState(installState, statePath, recoveryPath)
			Expect(err).Should(HaveOccurred())
		})
		It("Fails writing to recovery partition", func() {
			err = fs.RemoveAll(filepath.Dir(statePath))
			Expect(err).ShouldNot(HaveOccurred())
			err = config.WriteInstallState(installState, statePath, recoveryPath)
			Expect(err).Should(HaveOccurred())
		})
		It("Fails loading state file", func() {
			err = config.WriteInstallState(installState, statePath, recoveryPath)
			Expect(err).ShouldNot(HaveOccurred())
			err = fs.RemoveAll(filepath.Dir(statePath))
			_, err = config.LoadInstallState()
			Expect(err).Should(HaveOccurred())
		})
	})
})
