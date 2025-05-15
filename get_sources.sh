#!/bin/bash

# set -e

declare -a repos=(
    "https://github.com/william-xiang/kubeflow-ppc64le-manifests.git"
    "https://github.com/mgiessing/gpu-operator.git"
)

declare -a branches=(
    "main"
    "ppc64le_v1.10.1"
)

for i in ${!repos[@]}; do
    echo "Cloning repo ${repos[$i]} from branch ${branches[$i]}"
    git clone -b ${branches[$i]} --depth 1 ${repos[$i]}
    echo
done