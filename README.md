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
    Under the installed operators page for `Rocket AI Hub`, click `Create instance` button. In the `Create RocketAIHub` page, you can change the name of the new instance and other two options for optional components and identity provider. Then click `Create` button to create the instance.

    The identity provider is used for user authentication in the Rocket AI Hub instance. You can specify the name of an existing identity provider in the current cluster to use it for the new instance.
    If omitted, a Keycloak instance will be created and used as the identity provider. In this case, you can choose whether to create a default user in Keycloak. This default user is useful for testing and quick experimentation.
    For production environments, we recommend not creating the default user and instead allowing the administrator to create the users for team members. You can find instructions for creating new users in the next section.

    > **Note:** Only one Rocket AI Hub instance can be created in the same cluster. An error will appear when trying to create more than one instance.

4. Post installation steps

- Wait for the OpenShift authentication cluster operator to be ready  
  This step is required if you choose to use Keycloak as the identity provider. It may take several minutes to complete. Once finished, you can log in to the Rocket AI Hub instance using the new identity provider.
- Retrieve the URLs for the Rocket AI Hub and the Keycloak instance, and password for admin of Keycloak
  These two URLs can be found in the status of the new Rocket AI Hub instance. This can be done using Openshift web console or oc command. The admin password of Keycloak can also be found using oc command.
  Below are the oc commands:

  ```sh
  # Get Keycloak URL when using Keycloak as identity provider
  oc get rocketaihub rocketaihub -o jsonpath="{.status.keycloakURL}"
  # Get the URL for Rocket AI Hub instance
  oc get rocketaihub rocketaihub -o jsonpath="{.status.kubeflowURL}"
  # Get the admin password for Keycloak
  oc get secret credential-rocketaihub-keycloak -n rocketaihub-keycloak -o jsonpath="{.data.ADMIN_PASSWORD}" | base64 -d
  ```

- Create new users
  If Keycloak is used as the identity provider, the administrator must create new users. The Keycloak URL was retrieved in the previous step. New users should be created in the rocketaihub realm, and an email address must be specified for each user.

- Log into Rocket AI Hub instance  
  Use the URL retrieved in the previous step to log in to the Rocket AI Hub instance. Ensure that you log in using the identity provider specified during the installation.  
  If you are using Keycloak as the identity provider, it is normal to get a `403 Permission Denied` error on your first login. This occurs because OpenShift may take several seconds to provision the necessary resources for the new user. You can try logging in again after 30 seconds.

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
