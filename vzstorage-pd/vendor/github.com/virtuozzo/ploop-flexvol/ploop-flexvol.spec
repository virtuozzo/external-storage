%global with_devel 0
%global with_bundled 0
%global with_debug 0
%global with_check 0
%global with_unit_test 0

%if 0%{?with_debug}
%global _dwz_low_mem_die_limit 0
%else
%global debug_package   %{nil}
%endif

%global provider        github
%global provider_tld    com
%global project         virtuozzo
%global repo            ploop-flexvol
%global bin             ploop
# https://github.com/kolyshkin/docker-volume-ploop
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}
%global commit         	57b2f919e620fb62b9c7e49121f023141d38ffba
%global shortcommit     %(c=%{commit}; echo ${c:0:7})

Name:           %{repo}
Version:        0.1
Release:        1
Summary:        Ploop Flexvolume Plugin for Kubernetes
License:        MIT
URL:            https://%{provider_prefix}
Source: 	%{name}-%{version}.tar.gz

# e.g. el6 has ppc64 arch without gcc-go, so EA tag is required
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 %{arm}}
# If go_compiler is not set to 1, there is no virtual provide. Use golang instead.
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang}
BuildRequires: ploop-devel
BuildRequires: git

%description
%{summary}

%prep

%setup -q -n %{name}-%{version}

%build
ls -alh
mkdir -p %{buildroot}/usr/libexec/kubernetes/kubelet-plugins/volume/exec/%{project}~%{bin}
mkdir -p src/github.com/virtuozzo
ln -s ../../../ src/github.com/virtuozzo/ploop-flexvol
export GOPATH=$(pwd):%{gopath}
cd src/github.com/virtuozzo/ploop-flexvol
go build -o %{bin} main.go


%install
mkdir -p %{buildroot}/usr/libexec/kubernetes/kubelet-plugins/volume/exec/%{project}~%{bin}
%{__install} -m0755 ploop %{buildroot}/usr/libexec/kubernetes/kubelet-plugins/volume/exec/%{project}~%{bin}/%{bin}


%files
%defattr(-,root,root,755)
/usr/libexec/kubernetes/kubelet-plugins/volume/exec/%{project}~%{bin}/%{bin}

%clean

%post

#define license tag if not already defined
%{!?_licensedir:%global license %doc}

%changelog
* Thu Feb 09 2017 Unknown name 0.1-1
- new package built with tito
