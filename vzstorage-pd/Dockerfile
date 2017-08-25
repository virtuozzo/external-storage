FROM centos:7
ENTRYPOINT ["vzstorage-pd"]

ADD virtuozzo.repo /etc/yum.repos.d/
ADD vzlinux.repo /etc/yum.repos.d/
ADD openvz-factory.repo /etc/yum.repos.d/
RUN printf "upgrade \n install vstorage-ctl vstorage-client ploop gdisk \n run" | yum shell -y

ADD vzstorage-pd /usr/bin/
