# Rocket AI Hub operator

This is the operator for Rocket AI Hub which provides AI and machine learning (ML) capabilities optimized for IBM Power systems.

## Installation

### Supported platform

- OpenShift 4.14, 4.15 on ppc64le (newer versions haven’t been tested yet, you’re welcome to try them and report any issues to us)

### Prerequisites

1. Make sure that OpenShift CLI is installed if you want to install the operator using oc command. Follow the instructions in this [link](https://docs.redhat.com/en/documentation/openshift_container_platform/4.11/html/cli_tools/openshift-cli-oc#cli-getting-started) to install OpenShift CLI.

2. A default storage class should be configured properly on the cluster for storage dynamic provisioning.

### Installation steps

1. Create a catalog source  
    Catalog source can be created in OpenShift using Openshift web console or oc command.  
    Below is the content of the catalog source.

    ```yaml
    apiVersion: operators.coreos.com/v1alpha1
    kind: CatalogSource
    metadata:
      name: rocketaihub
      namespace: openshift-marketplace
    spec:
      displayName: 'Rocket AI Hub Catalog Source'
      image: 'quay.io/williamxiang/rocketaihub-operator-catalog:v0.0.1'
      publisher: 'IBM'
      sourceType: grpc
      updateStrategy:
        registryPoll:
          interval: 45m
    ```

2. Install the operator  
    The operator can be installed using the web console. After the catalog source is created and ready, search for `rocketaihub` in `OperatorHub` page, then click install button to install the Rocket AI Hub operator.

3. Create a Rocket AI Hub instance  
    After the operator is installed and ready, it's time to create a Rocket AI Hub instance. This can also be done using Openshift web console.  
    Under the installed operators page for `Rocket AI Hub`, click `Create instance` button. In the `Create RocketAIHub` page, you can change the name of the new instance and select the optional components to be installed. Then click `Create` button to create the instance.

    > **Note:** Only one Rocket AI Hub instance can be created in the same cluster. An error will appear when trying to create more than one instance.


## Developer guide

### Prerequisites

- go version v1.24.0+
- docker version 19.03+ with buildx enabled (refer to the [link](https://docs.docker.com/build/building/multi-platform/) for setting up environment for multi-platform builds)
- oc version v4.14+.
- Access to an OpenShift v4.14+ cluster on ppc64le.



### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-buildx IMG=<some-registry>/rocketaihub-operator:tag
```

>**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/rocketaihub-operator:tag
```

>**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instance of the operator**
You can apply the samples (examples) from the config/sample:

```sh
oc apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```
