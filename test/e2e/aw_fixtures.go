/*
Copyright 2019, 2021, 2022, 2023 The Multi-Cluster App Dispatcher Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
)

func createGenericAWTimeoutWithStatus(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-jobtimeout-with-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"spec": {
					"containers": [
						{
							"args": [
								"sleep infinity"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-jobtimeout-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	var schedSpecMin int32 = 1
	var dispatchDurationSeconds int32 = 10
	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
				NotImplemented_DispatchDuration: arbv1.NotImplemented_DispatchDurationSpec{
					Limit: dispatchDurationSeconds,
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createJobAWWithInitContainer(ctx context.Context, name string, requeuingTimeInSeconds int, requeuingGrowthType string, requeuingMaxNumRequeuings int) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "batch/v1",
		"kind": "Job",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"parallelism": 3,
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `"
				}
			},
			"spec": {
				"terminationGracePeriodSeconds": 1,
				"restartPolicy": "Never",
				"initContainers": [
					{
						"name": "job-init-container",
						"image": "quay.io/project-codeflare/busybox:latest",
						"command": ["sleep", "200"],
						"resources": {
							"requests": {
								"cpu": "500m"
							}
						}
					}
				],
				"containers": [
					{
						"name": "job-container",
						"image": "quay.io/project-codeflare/busybox:latest",
						"command": ["sleep", "10"],
						"resources": {
							"requests": {
								"cpu": "500m"
							}
						}
					}
				]
			}
		}
	}} `)

	var minAvailable int32 = 3

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: minAvailable,
				Requeuing: arbv1.RequeuingSpec{
					TimeInSeconds:             int64(requeuingTimeInSeconds),
					NotImplemented_GrowthType: requeuingGrowthType,
					MaxNumRequeuings:          int32(requeuingMaxNumRequeuings),
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"replicas": 3,
		"selector": {
			"matchLabels": {
				"app": "` + name + `"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 3

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithStatus(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	// var schedSpecMin int = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				// MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithMultipleStatus(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-ms-21-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-ms-21-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	rb2 := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-ms-21-2",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-ms-21-2",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb2,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createAWGenericItemWithoutStatus(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "scheduling.sigs.k8s.io/v1alpha1",
                        "kind": "PodGroup",
                        "metadata": {
                            "name": "aw-schd-spec-with-timeout-1",
                            "namespace": "default"
                        },
                        "spec": {
                            "minMember": 1
                        }
		}`)
	var schedSpecMin int32 = 1
	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithScheduleSpec(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-scheduling-spec",
			"namespace": "test"
		},
		"spec": {
			"completions": 2,
			"parallelism": 2,
			"template": {
				"spec": {
					"containers": [
						{
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"args": [
								"sleep 5"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-scheduling-spec",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWtWithLargeCompute(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-large-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "quay.io/quay/ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "10000m",
									"memory": "256M",
									"nvidia.com/gpu": "100"
								},
								"requests": {
									"cpu": "100000m",
									"memory": "256M",
									"nvidia.com/gpu": "100"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	// var schedSpecMin int = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				// MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						// CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericServiceAWWithNoStatus(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Service",
		"metadata": {
			"labels": {
				"appwrapper.mcad.ibm.com": "test-dep-job-item",
				"resourceName": "test-dep-job-item-svc"
			},
			"name": "test-dep-job-item-svc",
			"namespace": "test"
		},
		"spec": {
			"ports": [
				{
					"name": "client",
					"port": 10001,
					"protocol": "TCP",
					"targetPort": 10001
				},
				{
					"name": "dashboard",
					"port": 8265,
					"protocol": "TCP",
					"targetPort": 8265
				},
				{
					"name": "redis",
					"port": 6379,
					"protocol": "TCP",
					"targetPort": 6379
				}
			],
			"selector": {
				"component": "test-dep-job-item-svc"
			},
			"sessionAffinity": "None",
			"type": "ClusterIP"
		}
	}`)

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentAWWithMultipleItems(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "` + name + `-deployment-1",
			"namespace": "test",
			"labels": {
				"app": "` + name + `-deployment-1"
			}
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "` + name + `-deployment-1"
				}
			},
			"template": {
				"metadata": {
					"labels": {
						"app": "` + name + `-deployment-1"
					}
				},
				"spec": {
					"initContainers": [
						{
							"name": "job-init-container",
							"image": "quay.io/project-codeflare/busybox:latest",
							"command": ["sleep", "200"],
							"resources": {
								"requests": {
									"cpu": "500m"
								}
							}
						}
					],
					"containers": [
						{
							"name": "` + name + `-deployment-1",
							"image": "quay.io/project-codeflare/echo-server:1.0",
							"ports": [
								{
									"containerPort": 80
								}
							]
						}
					]
				}
			}
		}} `)
	rb1 := []byte(`{"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "` + name + `-deployment-2",
		"namespace": "test",
		"labels": {
			"app": "` + name + `-deployment-2"
		}
	},

	"spec": {
		"replicas": 1,
		"selector": {
			"matchLabels": {
				"app": "` + name + `-deployment-2"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `-deployment-2"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `-deployment-2",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)

	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Progressing",
					},
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb1,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentWithCPUAW(ctx context.Context, name string, cpuDemand *resource.Quantity, replicas int) *arbv1.AppWrapper {
	rb := []byte(fmt.Sprintf(`{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "%s",
		"namespace": "test",
		"labels": {
			"app": "%s"
		}
	},
	"spec": {
		"replicas": %d,
		"selector": {
			"matchLabels": {
				"app": "%s"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "%s"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `, name, name, replicas, name, name, name, cpuDemand))

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: int32(replicas),
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{CustomPodResources: []arbv1.CustomPodResource{
						{
							Replicas: int32(replicas),
							Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceCPU: *cpuDemand}},
					},
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentCustomPodResourcesWithCPUAW(ctx context.Context, name string, customPodCpuDemand string, cpuDemand string, replicas int, requeuingTimeInSeconds int) *arbv1.AppWrapper {
	rb := []byte(fmt.Sprintf(`{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "%s",
		"namespace": "test",
		"labels": {
			"app": "%s"
		}
	},
	"spec": {
		"replicas": %d,
		"selector": {
			"matchLabels": {
				"app": "%s"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "%s"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `, name, name, replicas, name, name, name, cpuDemand))

	var schedSpecMin int32 = int32(replicas)
	var customCpuResource = v1.ResourceList{"cpu": resource.MustParse(customPodCpuDemand)}

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
				Requeuing: arbv1.RequeuingSpec{
					TimeInSeconds: int64(requeuingTimeInSeconds),
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						CustomPodResources: []arbv1.CustomPodResource{
							{
								Replicas: int32(replicas),
								Requests: customCpuResource,
							},
						},
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createStatefulSetAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "StatefulSet",
	"metadata": {
		"name": "aw-statefulset-2",
		"namespace": "test",
		"labels": {
			"app": "aw-statefulset-2"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "aw-statefulset-2"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-statefulset-2"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-statefulset-2",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"imagePullPolicy": "Never",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadPodAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"labels": {
				"app": "aw-bad-podtemplate-2"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-bad-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 2,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createPodTemplateAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-podtemplate-1",
		"namespace": "test"
	},
	"spec": {
			"containers": [
				{
					"name": "aw-podtemplate-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	rb1 := []byte(`{"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-podtemplate-2",
		"namespace": "test"
	},
	"spec": {
			"containers": [
				{
					"name": "aw-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb1,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createPodCheckFailedStatusAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
	"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-checkfailedstatus-1",
		"namespace": "test"
	},
	"spec": {
			"containers": [
				{
					"name": "aw-checkfailedstatus-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			],
			"tolerations": [
				{
					"effect": "NoSchedule",
					"key": "key1",
					"value": "value1",
					"operator": "Equal"
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodAWCustomDemand(ctx context.Context, name string, cpuDemand string) *arbv1.AppWrapper {
	genericItems := fmt.Sprintf(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "%s",
			"namespace": "test",
			"labels": {
				"app": "%s"
			}
		},
		"spec": {
			"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"limits": {
								"cpu": "%s"
							},
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
			]
		}
	} `, name, name, name, cpuDemand, cpuDemand)

	rb := []byte(genericItems)
	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "aw-generic-pod-1",
			"namespace": "test",
			"labels": {
				"app": "aw-generic-pod-1"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-pod-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"resources": {
						"limits": {
							"memory": "150Mi"
						},
						"requests": {
							"memory": "150Mi"
						}
					},
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodTooBigAW(ctx context.Context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "aw-generic-big-pod-1",
			"namespace": "test",
			"labels": {
				"app": "aw-generic-big-pod-1"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-big-pod-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"resources": {
						"limits": {
							"cpu": "100",
							"memory": "150Mi"
						},
						"requests": {
							"cpu": "100",
							"memory": "150Mi"
						}
					},
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadGenericItemAW(ctx context.Context, name string) *arbv1.AppWrapper {
	// rb := []byte(`""`)
	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						// GenericTemplate: runtime.RawExtension{
						// 	Raw: rb,
						// },
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadGenericPodTemplateAW(ctx context.Context, name string) (*arbv1.AppWrapper, error) {
	rb := []byte(`{"metadata":
	{
		"name": "aw-generic-podtemplate-2",
		"namespace": "test",
		"labels": {
			"app": "aw-generic-podtemplate-2"
		}
	},
	"template": {
		"metadata": {
			"labels": {
				"app": "aw-generic-podtemplate-2"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						NotImplemented_Replicas: 2,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := getClient(ctx).Create(ctx, aw)
	Expect(err).To(HaveOccurred())
	return aw, err
}
