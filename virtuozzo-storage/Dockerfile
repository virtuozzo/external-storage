FROM centos:7
ENTRYPOINT ["virtuozzo-storage"]
CMD -master=http://127.0.0.1:8080 -kubeconfig=/kube/config

RUN mkdir -p /mnt/vstorage/kube/
ADD virtuozzo.repo /etc/yum.repos.d/
RUN yum upgrade -y
RUN yum install -y ploop gdisk

# RUN yum install -y ploop-flexvol
RUN mkdir -p /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop
ADD ploop /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop/ploop
RUN chmod x+o /usr/libexec/kubernetes/kubelet-plugins/volume/exec/jaxxstorm~ploop/ploop

ADD virtuozzo-storage /usr/local/bin/virtuozzo-storage
RUN chmod x+o /usr/local/bin/virtuozzo-storage
