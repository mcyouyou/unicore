#!/usr/bin/env bash
kubectl delete ns unicore
kubectl delete -f ../deploy.yaml
