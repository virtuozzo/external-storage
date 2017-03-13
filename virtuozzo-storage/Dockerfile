FROM centos:7
RUN mkdir -p /mnt/vstorage/kube/
ADD virtuozzo.repo /etc/yum.repos.d/
RUN yum upgrade -y
RUN yum install -y ploop gdisk

# RUN yum install -y ploop-flexvol
RUN mkdir -p /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop
ADD ploop /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop/ploop
CMD chmod x+o /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop/ploop

ADD virtuozzo-storage /usr/local/bin/virtuozzo-storage
CMD chmod x+o /usr/local/bin/virtuozzo-storage
