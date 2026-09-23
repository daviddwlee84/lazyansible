# lazyansible 操作指南：從瀏覽到執行

lazyansible 可以執行 playbook、role 和 ad-hoc module。**`Enter` 是查看，`r` 才是準備執行**；在執行確認畫面，預設選中 **Cancel**，按 `Tab` 選 **Run**，再按 `Enter` 才會啟動 Ansible。

如果現在只看到 YAML，表示你在 playbook 原始碼檢視器。按 `Esc` 回主畫面後，可以按大寫 **`O`** 開啟 Role Browser，或按 **`2` → `t`** 開啟目前 playbook 的 Tags Browser。

這份指南對應目前的 personal fork。Ansible 的 inventory、playbook、role、task、tag 概念，以及原生命令的使用方式，另見 [Ansible 基礎與原生操作](ansible-basics.zh-TW.md)。

範例中的 `lazyansible` 表示這份 fork 的程式；若尚未放進 PATH，可改用 checkout 裡的 `./bin/lazyansible`。以下兩個練習都從 lazyansible checkout 目錄輸入命令。

## 先找到正確的畫面

主畫面的四個區域可直接用數字切換。終端機較窄時只顯示目前區域；按 `1`–`4` 仍能切換。

| 位置 | 按鍵 | 結果 |
| --- | --- | --- |
| 主畫面 | `1` / `2` / `3` / `4` | Inventory / Playbooks / Status / Logs |
| 主畫面 | `Tab` / `Shift+Tab` | 切換焦點 |
| 清單 | `j/k` 或 `↓/↑` | 移動選擇 |
| 清單 | `g` 或 `gg`、`G` | 第一項、最後一項；也可用 Home / End |
| Inventory | `h/l` 或 `←/→` | 收合／回上層、展開／進入下一層 |
| Inventory | `Enter` | 查看所選 host/group 的 Ansible inventory 解析結果 |
| Inventory | `s` | 把所選 host/group 設成下一次執行的 `--limit` |
| Playbooks | `Enter` 或 Space | 查看 YAML 原始碼 |
| Playbooks | `r` | 檢查並顯示執行計畫，尚未執行 |
| Playbooks | `c` / `d` | 切換 Ansible `--check` / `--diff` |
| Playbooks | `t` / `e` | 選 tags / 設 extra-vars |
| 主畫面 | 大寫 `O` | Role Browser；是英文字母 O，不是數字 0 |
| 主畫面 | `:` | 搜尋可用操作，例如 roles、inventory、config、runtime |
| 主畫面 | `?` | 目前焦點可用的按鍵；長說明可用 `j/k` 捲動 |

`Enter` 檢視 host 不會同時設定執行範圍；要限定主機，另外按 `s`。Playbooks 區域會顯示目前的 limit、tags、check/diff 狀態。要清除 limit，先按 `:`，搜尋 `clear`，選 **Clear target limit**。

在 `/` 篩選欄、extra-vars 等文字欄位裡，`j`、`q`、`O`、`/` 都是文字。先按 `Enter` 結束篩選輸入，或按 `Esc` 返回，再使用導覽快捷鍵。若仍在 YAML 檢視器或其他彈出畫面，先按 `Esc` 回主畫面，再按 `2`、`t` 或 `O`。

### Role Browser：入口與能力

1. 在主畫面按大寫 `O`；也可先按 `:`，輸入 `role`，選 **Role browser**，按 `Enter`。
2. 左側用 `j/k` 選 role；`/` 可依 role 名稱篩選。輸入後按 `Enter` 回到清單操作。
3. `Enter`、`l` 或 `→` 把焦點移到右側；`j/k` 捲動內容。`h` 或 `←` 回左側；`Tab` 也可切換兩側。
4. `r` 準備執行整個所選 role，進入同一個執行確認畫面。若只想看，按 `Esc` 返回即可。

目前左側掃描的是 **`<工作目錄>/roles/` 下的直接子目錄**。右側把 role 的 `tasks/main.yml`、`defaults/main.yml`、`handlers/main.yml`、`meta/main.yml` 整理成 Tasks、Defaults、Handlers、Dependencies 摘要。

右側是可捲動的內容摘要，沒有可逐項點入的 task 樹；目前也不會遞迴展開 `include_tasks`、`import_tasks`、`when` 分支。只放在 collections 或其他 Ansible `roles_path` 的 role，不會自動出現在這個本機目錄瀏覽器。要看實際檔案，使用編輯器；要看原 playbook 的任務清單，使用原生 `ansible-playbook ... --list-tasks`。

**直接跑 role 會建立一份只呼叫該 role 的臨時 playbook。** 它沿用目前選定的 inventory、limit、check/diff、tags、extra-vars 等執行設定，但不會重建原 playbook 的 `vars`、`vars_files`、`pre_tasks`、play 層級 `become`、`gather_facts` 或其他 role 的前後順序。若目的是「在原專案流程裡只跑某一部分」，通常先用原 playbook 加 `--tags`，比較能保留原來的執行上下文。

### Tags Browser：實際選取流程

1. 回主畫面，按 `2` 聚焦 Playbooks，選定要執行的 playbook。
2. 按 `t`。用 `j/k` 選 tag，**Space 勾選／取消**。
3. 若清單很長，按 `/` 輸入篩選文字，再按 `Enter` 結束輸入；接著才用 Space 勾選。
4. 在清單操作狀態按 `Enter`，將選擇套用到執行設定；這一步不會執行 playbook。
5. 回主畫面按 `r`，在 review 確認 `Tags` 與命令中的 `--tags`。

`a` 選取目前篩選後的所有 tags；大寫 `A` 清除全部已選 tags。要恢復不傳 `--tags` 的狀態，按 `t` → `A` → `Enter`。切換 playbook 時也要留意原本的 tags 是否仍符合新的選擇。

目前 tags 清單來自**所選 YAML 檔內的靜態掃描**：例如該檔的 play、task、block、`roles:` 宣告上的 `tags:`。它不會讀進被匯入的 playbook、role 內部 task 檔，或解析動態 include。`No tags found` 只代表這個掃描沒有找到，不代表執行時完全沒有可用 tags。

可在 Ansible 專案目錄用原生命令交叉確認：

```sh
ansible-playbook -i inventory.ini site.yml --list-tags
ansible-playbook -i inventory.ini site.yml --tags greeting --list-tasks
```

原生 `--list-tags`／`--list-tasks` 也不會展開動態 include 裡的 tags/tasks，因此仍不能當成完整執行流程圖。相關繼承與 include 行為見 [Ansible 官方 Tags 文件](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_tags.html)。

若已知正確 tag，但 TUI 尚未掃描出來，可直接透過本工具的 CLI 指定：

```sh
lazyansible -C /path/to/ansible -i inventory.ini \
  run site.yml --tags greeting --dry-run
```

## 預覽、check mode、真正執行的差別

| 操作 | 會做什麼 |
| --- | --- |
| `Enter` 查看 YAML、開啟 Role Browser | 讀取本機內容；不啟動 playbook 執行 |
| Inventory / Config Inspector | 呼叫 Ansible 的 inventory/config 讀取命令；不是執行 playbook |
| TUI 的 `r`，或 CLI 的 `--dry-run` | 準備與驗證命令、解析使用的 runtime；不執行該 playbook／role／安裝或升級操作 |
| `c` 開啟 `--check`，再確認 Run | **會啟動 Ansible**，以 check mode 處理任務 |
| `d` 開啟 `--diff` | 要求支援的 module 顯示前後差異；本身不禁止變更 |
| Review 選 Run 再 `Enter` | 執行 review 中的計畫；效果依 playbook 與 check mode 等參數而定 |

`--dry-run` 是 lazyansible 的**命令計畫預覽**，`--check` 是 Ansible 的**模擬執行模式**。Check mode 的支援取決於 module；task 若明確指定 `check_mode: false`，即使整份 playbook 使用 `--check`，仍可正常執行。不要把 check mode 當成全域唯讀開關。見 [Ansible 官方 Check / Diff 文件](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_checkmode.html)。

這個 TUI 也有檔案編輯、profile 儲存、Galaxy 安裝、runtime 安裝／升級功能。因此「目前在看原始碼」和「整個工具是唯讀」是不同的使用狀態。

## 練習一：用附帶的 localhost 教學專案

在 lazyansible checkout 目錄啟動：

```sh
# 尚未建置時執行一次
mkdir -p bin
go build -o bin/lazyansible ./cmd/lazyansible

./bin/lazyansible -C testdata/tutorial -i inventory.ini -d . \
  --check=false --diff=false --check-updates=false
```

教學專案只有本機 `localhost`，使用 `ansible_connection=local`。`site.yml` 呼叫 `demo` role，role 宣告上的 tag 是 `greeting`；另一個 debug task 的 tag 是 `summary`。教學任務使用 debug 輸出，不安裝套件或修改系統設定。

### 跑一次指定 tag

1. 按 `1`，用 `j/k` 選 `localhost`，按 `s` 將 limit 設為該主機。
2. 按 `2`，選 `site`。按 `Enter` 看 YAML，再按 `Esc` 回來。
3. 按 `t` → `/`，輸入 `greeting` → `Enter` 結束篩選 → Space 勾選 → `Enter` 套用。
4. 按 `r`。檢查工作目錄為 `testdata/tutorial`、inventory 為教學檔、limit 為 `localhost`、tags 為 `greeting`。
5. 第一次可直接按 `Enter`：因為預設選的是 Cancel，會取消回來。
6. 再按 `r`，等計畫準備完成，按 `Tab` 選 Run，再按 `Enter` 執行。
7. 按 `4` 看 Logs，應看到 `Message supplied by site.yml`。按 `3` 可看 host 狀態。

同樣的流程也能用 CLI 操作：

```sh
# 只看計畫
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  run site.yml --limit localhost --tags greeting --dry-run

# 顯示計畫後，在終端機詢問一次是否執行
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  run site.yml --limit localhost --tags greeting
```

### 看 role，再體驗「直接跑 role」的差異

先按 `2` → `t` → `A` → `Enter` 清除剛才的 tag，再按大寫 `O`。選 `demo`，用 `Enter`／`Tab` 看 Tasks 與 Defaults 摘要；這裡的 role 預設訊息是 `Default message from role demo`。

若在 Role Browser 按 `r`，review 後選 Run，執行的是臨時 playbook 呼叫的 `demo` role，訊息會使用 role 的預設值。原本 `site.yml` 裡的 `demo_message` 不會跟著複製過來。

產生的 playbook 也沒有沿用 `site.yml` 的 `gather_facts: false`，所以這條直接 role 路徑可能先顯示 Gathering Facts；這同樣是執行上下文不同的結果。

先清除 `greeting` 很重要：這個 tag 是 `site.yml` 的 role 宣告加上的，不是 `demo/tasks/main.yml` 自己的 task tag。直接 role 執行若仍帶 `--tags greeting`，可能把預期任務篩掉。

```sh
# 直接 role 執行的命令計畫
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  role run roles/demo --hosts local --dry-run
```

原有的 `testdata/localhost/smoke.yml` 用來展示 ok、changed、skipped、ignored failure 輸出，**刻意沒有 tags 或 roles**；在那個 fixture 看不到兩者是正常的。要學 `O`／`t`，請用 `testdata/tutorial`。

## 練習二：打開自己的 dotfiles Ansible 專案

本機這份 dotfiles 的 Ansible 工作目錄是 `~/.ansible`，playbook 是 `playbooks/macos.yml`，inventory 是 `inventories/localhost.ini`；inventory 的 `local` group 包含 `localhost ansible_connection=local`。這個練習只瀏覽與準備命令，不套用 dotfiles。

```sh
./bin/lazyansible -C "$HOME/.ansible" \
  -i inventories/localhost.ini -d playbooks \
  --check --diff --check-updates=false
```

1. 按 `2`，選 `macos`。若清單很長，用 `/` 輸入 `macos`，按 `Enter` 結束篩選。再按 `Enter` 才是查看 YAML，`Esc` 回來。
2. 按 `t`。這份檔案的 role 宣告帶有 `homebrew`、`base`、`zsh`、`neovim`、`devtools`、`python_uv_tools` 等 tags，應能在清單中看到。
3. 在 Tags Browser 按 `/`，輸入 `neovim` → `Enter` → Space → `Enter`，把這個 tag 套用到下一次計畫。
4. 按 `1`，選 `localhost`，按 `s`；再按 `2` 返回 playbook。啟動命令已開啟 check/diff，這裡不必再按 `c/d` 把它們關掉。
5. 按 `r`，確認 cwd 是 `~/.ansible`、playbook 是 `playbooks/macos.yml`、limit 是 `localhost`、tag 是 `neovim`。**保留 Cancel，按 `Enter` 回來**，這次只練習到計畫。
6. 按大寫 `O`，用 `/` 找 `neovim` role；結束篩選後用 `Enter` 看右側摘要。用 `Esc` 返回。這裡能瀏覽 `~/.ansible/roles/neovim`，不需要直接執行 role。

如果按 `t` 顯示空白，先確認目前選中的是 `macos`，而且已離開 YAML 檢視器／篩選輸入；若按 `O` 找不到 role，確認 `-C` 是 `~/.ansible`，而不是 `~/.ansible/playbooks`。

同一個計畫可從 CLI 確認，`--dry-run` 不會啟動這份 playbook：

```sh
./bin/lazyansible -C "$HOME/.ansible" -i inventories/localhost.ini \
  run playbooks/macos.yml --limit localhost --tags neovim \
  --check --diff --dry-run

# 讀取本專案 Ansible 的 config / inventory
./bin/lazyansible -C "$HOME/.ansible" inspect config
./bin/lazyansible -C "$HOME/.ansible" -i inventories/localhost.ini \
  -d playbooks inspect inventory
```

原生 Ansible 的 tag／task 清單則從同一個工作目錄查看：

```sh
(
  cd "$HOME/.ansible" &&
  ansible-playbook -i inventories/localhost.ini playbooks/macos.yml --list-tags
)

(
  cd "$HOME/.ansible" &&
  ansible-playbook -i inventories/localhost.ini playbooks/macos.yml \
    --limit localhost --tags neovim --list-tasks
)
```

### 與原本 chezmoi 操作的關係

這份 dotfiles 的 chezmoi `run_onchange` bridge 會依 OS、偏好設定組合 tags、extra-vars，以及 sudo／no-root、Python interpreter、PATH 等執行環境；只有相關 on-change 條件觸發時才會執行。**lazyansible 不會自動讀入或重建這套 chezmoi 選擇。**

`just ansible-macos` 本身也是切到 `~/.ansible` 後執行 `ansible-playbook playbooks/macos.yml`，不是完整偏好設定流程的替代入口。要維持原本整套 dotfiles 套用語意，繼續使用該專案既有的 chezmoi 流程；lazyansible 很適合先檢視 playbook／roles，或在明確指定 host、tags、vars 後執行其中一段。

此外，本機 `ansible.cfg` 的 `stdout_callback=clean` 會影響原生命令的輸出。lazyansible 會為自己的子程序覆寫成 `default` callback，因此日誌外觀不同，並不代表它修改了你的 dotfiles 設定。

## 分清楚三種路徑

```sh
lazyansible -C /project/ansible -i inventories/local.ini -d playbooks
```

| 參數 | 這個例子的意思 |
| --- | --- |
| `-C /project/ansible` | Ansible 子程序的工作目錄，也是 Role Browser 尋找 `roles/` 的基準 |
| `-i inventories/local.ini` | 使用 `/project/ansible/inventories/local.ini` 當 inventory |
| `-d playbooks` | 在 `/project/ansible/playbooks` 搜尋 playbook；不會把工作目錄改到這裡 |

**兩個程式的 `-C` 意思不同：** `lazyansible -C` 是 `--chdir`；原生 `ansible-playbook -C` 是 `--check`。原生命令範例使用 shell 的 `cd` 設定工作目錄，避免混用。

因此把 `-d` 指向正確位置，只解決 playbook 清單；如果 `O` 顯示找不到 roles，還要確認 `-C` 下是否真的有 `roles/`。設定檔中的相對 inventory／playbook 目錄也以工作目錄為基準，不是相對 `~/.config/lazyansible/`。

Inventory Inspector 會額外把目前選中 playbook 所在目錄當作 `ansible-inventory --playbook-dir`；沒有選中 playbook 時，才使用設定的 playbook 目錄或工作目錄。這影響 Ansible 尋找相關變數檔的上下文，但不等於 task 執行時的全部變數。

## 選擇工具入口

| 想做的事 | 建議入口 |
| --- | --- |
| 看有哪些本機 playbook、roles，選 host/tag 後執行 | lazyansible TUI |
| 已知所有參數，想先看命令、再執行或寫腳本 | `lazyansible run ... --dry-run`，確認後再執行 |
| 使用本工具尚未提供的 Ansible flags，例如 `--list-tasks`、`--skip-tags` | 原生 `ansible-playbook` |
| 查看 inventory 解析結果、變更過的 Ansible config | `:` 的 Inspector，或 `lazyansible inspect inventory/config` |
| 看目前用哪個 Ansible、Python、uv tool owner | `:` 搜尋 `runtime`，或 `lazyansible runtime status` |

`:` 的操作方式是先開 Actions，再輸入關鍵字，用 `↑/↓` 選操作、`Enter` 開啟。Runtime 中 `c` 檢查更新，`u` 準備升級計畫；一般開啟畫面不會自動安裝或升級。要在 CLI 僅檢查：

```sh
lazyansible runtime status
lazyansible runtime check
lazyansible runtime upgrade --dry-run
```

更多設定、XDG 路徑與版本管理方式見 [README](../README.md)。完整流程圖、`when` 分支與變數來源追蹤仍列在 [後續評估](../backlog/variable-provenance-graphs.md)，目前的 YAML／role 摘要不表示這些能力已實作。
