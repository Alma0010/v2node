#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

set -o pipefail
cur_dir=$(pwd)

V2NODE_REPOSITORY="Alma0010/v2node"
DEFAULT_GITHUB_PROXY="https://v4.gh-proxy.org/"
DOWNLOAD_PROXY=""
PROXY_ARG_SET=0

load_download_proxy() {
    if [[ "$PROXY_ARG_SET" != 1 ]]; then
        if [[ -n "${V2NODE_GITHUB_PROXY+x}" ]]; then
            DOWNLOAD_PROXY="$V2NODE_GITHUB_PROXY"
        elif [[ -r /etc/v2node/download-proxy ]]; then
            IFS= read -r DOWNLOAD_PROXY < /etc/v2node/download-proxy || true
        fi
    fi
    DOWNLOAD_PROXY="${DOWNLOAD_PROXY%$'\r'}"
    if [[ -n "$DOWNLOAD_PROXY" ]]; then
        if [[ ! "$DOWNLOAD_PROXY" =~ ^https://[^[:space:]\?\#]+/?$ ]]; then
            echo "下载反代地址必须是 HTTPS URL" >&2
            return 1
        fi
        DOWNLOAD_PROXY="${DOWNLOAD_PROXY%/}/"
    fi
}

github_download_url() {
    printf '%s%s\n' "$DOWNLOAD_PROXY" "$1"
}

download_file() {
    curl --fail --location --show-error --silent --connect-timeout 10 \
        --max-time 300 --retry 2 --retry-delay 1 \
        --output "$2" "$(github_download_url "$1")"
}

validate_shell_script() {
    [[ "$(head -n 1 "$1" | tr -d '\r')" == '#!/bin/bash' ]] && bash -n "$1"
}


########################
# 参数解析
########################
VERSION_ARG=""
API_HOST_ARG=""
NODE_ID_ARG=""
API_KEY_ARG=""

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --api-host)
                [[ $# -ge 2 && "$2" != --* ]] || { echo "参数 --api-host 缺少值" >&2; exit 1; }
                API_HOST_ARG="$2"; shift 2 ;;
            --node-id)
                [[ $# -ge 2 && "$2" != --* ]] || { echo "参数 --node-id 缺少值" >&2; exit 1; }
                NODE_ID_ARG="$2"; shift 2 ;;
            --api-key)
                [[ $# -ge 2 && "$2" != --* ]] || { echo "参数 --api-key 缺少值" >&2; exit 1; }
                API_KEY_ARG="$2"; shift 2 ;;
            --proxy)
                DOWNLOAD_PROXY="$DEFAULT_GITHUB_PROXY"; PROXY_ARG_SET=1; shift ;;
            --proxy=*)
                DOWNLOAD_PROXY="${1#*=}"; PROXY_ARG_SET=1; shift ;;
            --direct)
                DOWNLOAD_PROXY=""; PROXY_ARG_SET=1; shift ;;
            -h|--help)
                echo "用法: $0 [版本号] [--api-host URL] [--node-id ID] [--api-key KEY] [--proxy[=URL] | --direct]"
                exit 0 ;;
            --*)
                echo "未知参数: $1"; exit 1 ;;
            *)
                # 兼容第一个位置参数作为版本号
                if [[ -z "$VERSION_ARG" ]]; then
                    VERSION_ARG="$1"; shift
                else
                    echo "多余参数: $1" >&2; exit 1
                fi ;;
        esac
    done
}

detect_platform() {
# check root
[[ $EUID -ne 0 ]] && echo -e "${red}错误：${plain} 必须使用root用户运行此脚本！\n" && exit 1

# check os
if [[ -f /etc/redhat-release ]]; then
    release="centos"
elif cat /etc/issue | grep -Eqi "alpine"; then
    release="alpine"
elif cat /etc/issue | grep -Eqi "debian"; then
    release="debian"
elif cat /etc/issue | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /etc/issue | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "debian"; then
    release="debian"
elif cat /proc/version | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /proc/version | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "arch"; then
    release="arch"
else
    echo -e "${red}未检测到系统版本，请联系脚本作者！${plain}\n" && exit 1
fi

arch=$(uname -m)

if [[ $arch == "x86_64" || $arch == "x64" || $arch == "amd64" ]]; then
    arch="64"
elif [[ $arch == "aarch64" || $arch == "arm64" ]]; then
    arch="arm64-v8a"
elif [[ $arch == "s390x" ]]; then
    arch="s390x"
else
    arch="64"
    echo -e "${red}检测架构失败，使用默认架构: ${arch}${plain}"
fi

if [ "$(getconf WORD_BIT)" != '32' ] && [ "$(getconf LONG_BIT)" != '64' ] ; then
    echo "本软件不支持 32 位系统(x86)，请使用 64 位系统(x86_64)，如果检测有误，请联系作者"
    exit 2
fi

# os version
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F'[= ."]' '/VERSION_ID/{print $3}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F'[= ."]+' '/DISTRIB_RELEASE/{print $2}' /etc/lsb-release)
fi

if [[ x"${release}" == x"centos" ]]; then
    if [[ ${os_version} -le 6 ]]; then
        echo -e "${red}请使用 CentOS 7 或更高版本的系统！${plain}\n" && exit 1
    fi
    if [[ ${os_version} -eq 7 ]]; then
        echo -e "${red}注意： CentOS 7 无法使用hysteria1/2协议！${plain}\n"
    fi
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        echo -e "${red}请使用 Ubuntu 16 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        echo -e "${red}请使用 Debian 8 或更高版本的系统！${plain}\n" && exit 1
    fi
fi

}

install_base() {
    # 优化版本：批量检查和安装包，减少系统调用
    need_install_apt() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(dpkg-query -W -f='${Package}\n' 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            apt-get update -y >/dev/null 2>&1
            DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}" >/dev/null 2>&1
        fi
    }

    need_install_yum() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(rpm -qa --qf '%{NAME}\n' 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            yum install -y "${missing[@]}" >/dev/null 2>&1
        fi
    }

    need_install_apk() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(apk info 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            apk add --no-cache "${missing[@]}" >/dev/null 2>&1
        fi
    }

    # 一次性安装所有必需的包
    if [[ x"${release}" == x"centos" ]]; then
        # 检查并安装 epel-release
        if ! rpm -q epel-release >/dev/null 2>&1; then
            echo "安装 EPEL 源..."
            yum install -y epel-release >/dev/null 2>&1
        fi
        need_install_yum wget curl unzip tar cronie socat ca-certificates pv
        update-ca-trust force-enable >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"alpine" ]]; then
        need_install_apk wget curl unzip tar socat ca-certificates pv
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"debian" ]]; then
        need_install_apt wget curl unzip tar cron socat ca-certificates pv
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"ubuntu" ]]; then
        need_install_apt wget curl unzip tar cron socat ca-certificates pv
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"arch" ]]; then
        echo "更新包数据库..."
        pacman -Sy --noconfirm >/dev/null 2>&1
        # --needed 会跳过已安装的包，非常高效
        echo "安装必需的包..."
        pacman -S --noconfirm --needed wget curl unzip tar cronie socat ca-certificates pv >/dev/null 2>&1
    fi
}

# 0: running, 1: not running, 2: not installed
check_status() {
    if [[ ! -f /usr/local/v2node/v2node ]]; then
        return 2
    fi
    if [[ x"${release}" == x"alpine" ]]; then
        temp=$(service v2node status | awk '{print $3}')
        if [[ x"${temp}" == x"started" ]]; then
            return 0
        else
            return 1
        fi
    else
        temp=$(systemctl status v2node | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
        if [[ x"${temp}" == x"running" ]]; then
            return 0
        else
            return 1
        fi
    fi
}

generate_v2node_config() {
        local api_host="$1"
        local node_id="$2"
        local api_key="$3"

        mkdir -p /etc/v2node >/dev/null 2>&1
        cat > /etc/v2node/config.json <<EOF
{
    "Log": {
        "Level": "warning",
        "Output": "",
        "Access": "none"
    },
    "Nodes": [
        {
            "ApiHost": "${api_host}",
            "NodeID": ${node_id},
            "ApiKey": "${api_key}",
            "Timeout": 15
        }
    ]
}
EOF
        echo -e "${green}V2node 配置文件生成完成,正在重新启动服务${plain}"
        if [[ x"${release}" == x"alpine" ]]; then
            service v2node restart
        else
            systemctl restart v2node
        fi
        sleep 2
        echo -e ""
        if check_status; then
            echo -e "${green}v2node 重启成功${plain}"
        else
            echo -e "${red}v2node 可能启动失败，请使用 v2node log 查看日志信息${plain}"
        fi
}

install_v2node() {
    local version_param="$1"

    if [[ -z "$version_param" || "$version_param" == latest ]]; then
        local metadata
        # This API resolves the tag only. Every downloaded file below uses the
        # selected route and an explicit release tag, never /latest/download/.
        metadata=$(curl --fail --location --show-error --silent --connect-timeout 10 \
            --max-time 30 --retry 2 "https://api.github.com/repos/${V2NODE_REPOSITORY}/releases/latest") || {
            echo "查询版本失败，可指定版本号后重试" >&2; exit 1;
        }
        last_version=$(printf '%s\n' "$metadata" | sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"[:space:]]+)".*/\1/p' | head -n1)
    else
        last_version="v${version_param#v}"
    fi
    [[ "$last_version" =~ ^v?[0-9][0-9A-Za-z._-]*$ ]] || { echo "无效的发行版版本号" >&2; exit 1; }
    mkdir -p /usr/local
    download_dir=$(mktemp -d /usr/local/.v2node-download.XXXXXX) || exit 1
    cleanup_download() {
        if [[ -n "${download_dir:-}" && "$download_dir" == /usr/local/.v2node-download.* ]]; then
            rm -rf -- "$download_dir"
        fi
    }
    trap cleanup_download EXIT
    url="https://github.com/${V2NODE_REPOSITORY}/releases/download/${last_version}/v2node-linux-${arch}.zip"
    echo "下载 ${last_version}，线路: ${DOWNLOAD_PROXY:-GitHub 直连}"
    download_file "$url" "$download_dir/v2node.zip" || { echo "下载发行版失败，旧版本未替换" >&2; exit 1; }
    unzip -tq "$download_dir/v2node.zip" >/dev/null || { echo "发行版压缩包校验失败" >&2; exit 1; }
    unzip -oq "$download_dir/v2node.zip" -d "$download_dir" || exit 1
    [[ -s "$download_dir/v2node" && -s "$download_dir/geoip.dat" && -s "$download_dir/geosite.dat" ]] || {
        echo "发行版缺少程序或资源文件" >&2; exit 1;
    }
    if [[ ! -f /etc/v2node/config.json && ! -s "$download_dir/config.json" ]]; then
        echo "发行版缺少配置模板" >&2; exit 1
    fi
    download_file "https://raw.githubusercontent.com/${V2NODE_REPOSITORY}/${last_version}/script/v2node.sh" "$download_dir/v2node.sh" || exit 1
    validate_shell_script "$download_dir/v2node.sh" || { echo "管理脚本校验失败" >&2; exit 1; }
    mkdir -p /usr/local/v2node /etc/v2node
    # Replace only after the complete release and management script are ready.
    install -m 755 "$download_dir/v2node" /usr/local/v2node/v2node.new || exit 1
    mv -f /usr/local/v2node/v2node.new /usr/local/v2node/v2node || exit 1
    cp "$download_dir/geoip.dat" /etc/v2node/ || exit 1
    cp "$download_dir/geosite.dat" /etc/v2node/ || exit 1
    install -m 755 "$download_dir/v2node.sh" /usr/bin/v2node.new || exit 1
    mv -f /usr/bin/v2node.new /usr/bin/v2node || exit 1
    printf '%s\n' "$DOWNLOAD_PROXY" > /etc/v2node/download-proxy
    chmod 600 /etc/v2node/download-proxy
    if [[ x"${release}" == x"alpine" ]]; then
        rm /etc/init.d/v2node -f
        cat <<EOF > /etc/init.d/v2node
#!/sbin/openrc-run

name="v2node"
description="v2node"

command="/usr/local/v2node/v2node"
command_args="server"
command_user="root"

pidfile="/run/v2node.pid"
command_background="yes"

depend() {
        need net
}
EOF
        chmod +x /etc/init.d/v2node
        rc-update add v2node default
        echo -e "${green}v2node ${last_version}${plain} 安装完成，已设置开机自启"
    else
        rm /etc/systemd/system/v2node.service -f
        cat <<EOF > /etc/systemd/system/v2node.service
[Unit]
Description=v2node Service
After=network.target nss-lookup.target
Wants=network.target

[Service]
User=root
Group=root
Type=simple
LimitAS=infinity
LimitRSS=infinity
LimitCORE=infinity
LimitNOFILE=999999
WorkingDirectory=/usr/local/v2node/
ExecStart=/usr/local/v2node/v2node server
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl stop v2node
        systemctl enable v2node
        echo -e "${green}v2node ${last_version}${plain} 安装完成，已设置开机自启"
    fi

    if [[ ! -f /etc/v2node/config.json ]]; then
        # 如果通过 CLI 传入了完整参数，则直接生成配置并跳过交互
        if [[ -n "$API_HOST_ARG" && -n "$NODE_ID_ARG" && -n "$API_KEY_ARG" ]]; then
            generate_v2node_config "$API_HOST_ARG" "$NODE_ID_ARG" "$API_KEY_ARG"
            echo -e "${green}已根据参数生成 /etc/v2node/config.json${plain}"
            first_install=false
        else
            cp "$download_dir/config.json" /etc/v2node/ || { echo "发行版缺少配置模板" >&2; exit 1; }
            first_install=true
        fi
    else
        if [[ x"${release}" == x"alpine" ]]; then
            service v2node start
        else
            systemctl start v2node
        fi
        sleep 2
        echo -e ""
        if check_status; then
            echo -e "${green}v2node 重启成功${plain}"
        else
            echo -e "${red}v2node 可能启动失败，请使用 v2node log 查看日志信息${plain}"
        fi
        first_install=false
    fi


    cleanup_download
    trap - EXIT
    cd "$cur_dir"
    echo "------------------------------------------"
    echo -e "管理脚本使用方法: "
    echo "------------------------------------------"
    echo "v2node              - 显示管理菜单 (功能更多)"
    echo "v2node start        - 启动 v2node"
    echo "v2node stop         - 停止 v2node"
    echo "v2node restart      - 重启 v2node"
    echo "v2node status       - 查看 v2node 状态"
    echo "v2node enable       - 设置 v2node 开机自启"
    echo "v2node disable      - 取消 v2node 开机自启"
    echo "v2node log          - 查看 v2node 日志"
    echo "v2node generate     - 生成 v2node 配置文件"
    echo "v2node update       - 更新 v2node"
    echo "v2node update x.x.x - 更新 v2node 指定版本"
    echo "v2node install      - 安装 v2node"
    echo "v2node uninstall    - 卸载 v2node"
    echo "v2node version      - 查看 v2node 版本"
    echo "------------------------------------------"

    if [[ $first_install == true ]]; then
        read -rp "检测到你为第一次安装 v2node，是否自动生成 /etc/v2node/config.json？(y/n): " if_generate
        if [[ "$if_generate" =~ ^[Yy]$ ]]; then
            # 交互式收集参数，提供示例默认值
            read -rp "面板API地址[格式: https://example.com/]: " api_host
            api_host=${api_host:-https://example.com/}
            read -rp "节点ID: " node_id
            node_id=${node_id:-1}
            read -rp "节点通讯密钥: " api_key

            # 生成配置文件（覆盖可能从包中复制的模板）
            generate_v2node_config "$api_host" "$node_id" "$api_key"
        else
            echo "${green}已跳过自动生成配置。如需后续生成，可执行: v2node generate${plain}"
        fi
    fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    parse_args "$@"
    load_download_proxy || exit 1
    detect_platform
    echo -e "${green}开始安装${plain}"
    install_base
    install_v2node "$VERSION_ARG"
fi
