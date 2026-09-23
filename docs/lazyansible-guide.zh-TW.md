# lazyansible 操作指南：從瀏覽到執行

lazyansible 可以執行 playbook、role 和 ad-hoc module。先記住一條路徑：**選 playbook → `t` 調整 tags → `p` 觀察範圍 → `r` 確認 → Logs 看結果**。主畫面的 Enter 用來查看；Run review 預設選 **Cancel**，Tab 選 **Run** 後再按 Enter 才開始執行。

大寫 **`O`** 開啟工作區的 Roles 分頁，不會離開目前的 playbook 上下文。Roles 裡的 **`r` 也是 review 目前 playbook**；直接跑單一 role 已移到明確標示的 Actions 操作。

這份指南對應目前的 personal fork。Ansible 的 inventory、playbook、role、task、tag 概念，以及原生命令的使用方式，另見 [Ansible 基礎與原生操作](ansible-basics.zh-TW.md)。

範例中的 `lazyansible` 表示這份 fork 的程式；若尚未放進 PATH，可改用 checkout 裡的 `./bin/lazyansible`。以下兩個練習都從 lazyansible checkout 目錄輸入命令。

## 先找到正確的畫面

主畫面有 Inventory、Playbooks、Status，以及含 **Logs / Roles / Preview** 的工作區。寬終端顯示上方資源區與下方工作區；窄終端顯示目前焦點。工作區的上下文列保留 playbook、inventory、limit、tags、模式和 cwd。

| 位置 | 按鍵 | 結果 |
| --- | --- | --- |
| 主畫面 | `1` / `2` / `3` | Inventory / Playbooks / Status |
| 主畫面 | Tab / Shift+Tab | 切換主要區域焦點 |
| 主畫面 | `4` / 大寫 `O` / `p` | Logs / Roles / 重新觀察 Preview |
| 工作區 | `[` / `]`、`Z` | 切換保留狀態的分頁、放大工作區 |
| 清單 | `j/k` 或 `↓/↑`、`g` / `gg`、`G` | 移動、第一項、最後一項 |
| Inventory | `h/l` 或 `←/→`、Space | 收合／展開群組 |
| Inventory | Enter、`s` | 檢視 host/group；設定下一次的 `--limit` |
| Playbooks | Enter 或 Space | 查看 YAML |
| Playbooks／工作區 | `t`、`r` | 編輯 tags 草稿、review 目前 playbook |
| Playbooks／工作區 | `c` / `d` / `e` | Check / diff / extra-vars |
| Roles／Preview | Enter 或 `l`、`h` | 進入內容／右欄、回清單／左欄 |
| Roles | `a` / `f` / `s` | 相關宣告／全部專案 roles、原始檔、建議宣告中的 tags |
| 主畫面 | `:`、`?` | Actions、可捲動的 Help |

`?` 是 **Help**，不是 Settings；要查看應用偏好，按 `:` 搜尋 `settings`。Help 裡可用 `j/k`、`g/G` 捲動。

Enter 查看 host 不會同時更動執行範圍，另外按 `s` 才會設定 limit；清除時用 Actions 的 **Clear target limit**。文字欄位裡的 `j`、`q`、`O`、`/` 都是文字，Enter 結束篩選輸入後才恢復導覽。

Tab 留給主要焦點切換；Roles 和 Preview 內部使用 `h/l` 與 Enter。YAML 檢視器可用 Esc 返回；工作區分頁則用 `[` / `]`、`4`、`O` 切換，或按 `2` 回 Playbooks。

### Roles：先看與目前 playbook 的關係

1. 選定 playbook 後按大寫 `O`，或 Actions → **Role browser**。
2. 預設是 **Related declarations**：目前檔案中觀察到的 role 宣告。相同 role 出現兩次仍是兩列，各自保留 play、宣告行與 tags。
3. `j/k` 選列，Enter／`l` 看摘要；`h` 回清單。窄畫面會在清單與內容間切換。
4. `a` 切成 **Project roles**，查看 `<工作目錄>/roles/` 的直接子目錄；再按 `a` 回相關宣告。兩種清單各自保留篩選與選擇。
5. `f` 開啟原始檔清單：宣告所在 playbook，以及可找到的 tasks/defaults/vars/handlers/meta 主檔。Enter 開啟內容；Esc 回檔案清單，再 Esc 回 Roles。

目前關聯只觀察所選檔案中的 literal `roles:` 宣告。Imports、includes、Jinja role 名稱會標示未解析，不會遞迴追蹤；collections 和其他 `roles_path` 也不會被當成本機 `roles/` 自動掃描。沒有直接宣告不代表 playbook 沒有使用該 role。

`r` 保留目前 playbook 上下文，進入它的 Run review。`t` 開啟標準 Tags 草稿；`s` 或 Actions 的 **Use this role's declared playbook tags**，則以這一列實際觀察到的宣告 tags 預填同一個 picker。Enter 會以這份草稿取代目前選擇，且同一 tag 可能選到其他 tasks，不能視為「只跑這個 role」。

**單獨執行 role 是另一個明確選擇。** 選可用的本機 role 後，按 `:` 搜尋 `standalone`，選 **Review standalone role (separate playbook context)**。它會為這份獨立計畫省略目前 playbook 的 tag filter，建立新的 role play，review 會標示上下文差異。它沿用目前 inventory、limit、check/diff、extra-vars，但不複製原 playbook 的 vars、vars_files、pre_tasks、play 層級 become、gather_facts 或其他 role 的順序。

Esc 是分層返回：先退出輸入；原始檔預覽回檔案清單，再回 Roles；角色內容回左欄；清掉剩餘 filter。Roles 是保留的分頁，回 Playbooks 直接按 `2`，不必靠多次 Esc 關閉它。

### Tags Browser：實際選取流程

1. 回主畫面，按 `2` 聚焦 Playbooks，選定要執行的 playbook。
2. 按 `t`。用 `j/k` 選 tag，**Space 勾選／取消**。
3. 若清單很長，按 `/` 輸入篩選文字，再按 `Enter` 結束輸入；接著才用 Space 勾選。
4. 在清單操作狀態按 `Enter`，將選擇套用到執行設定；這一步不會執行 playbook。
5. 回主畫面按 `r`，在 review 確認 `Tags` 與命令中的 `--tags`。

`a` 選取目前篩選後的所有 tags；大寫 `A` 清除草稿。按 `t` → `A` → Enter 會套用「沒有 UI tag filter」。Esc 先退出／清除篩選，再取消整份草稿；取消保留原本已套用的 tags。套用後回到原來的工作區分頁、焦點和 playbook，狀態列會顯示結果。

Tags 會立即顯示本地 YAML 與已選 tags，再非同步呼叫 Ansible `--list-tags` 補充 catalogue。這個 catalogue 刻意不帶 UI 目前的 tag 選擇，但仍尊重繼承的 Ansible 設定／環境 filter。Discovery 失敗不會清空草稿；已選但未觀察到的 tag 會保留並標記。

每個 playbook 的已套用 tags 在本次 session 中分別記住。切換 playbook 再切回來可繼續原本的選擇；這不是持久化 profile。改 tags 會讓舊 Preview 標示 stale，**不會自動跑 Preview 或 check mode**。

原生清單仍可能漏掉 dynamic include 的子 tasks/tags，所以空清單不代表執行時絕對沒有 tags。詳見 [官方 Tags 文件](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_tags.html)。CLI 可直接查同一份 catalogue：

```sh
lazyansible -C /path/to/ansible -i inventory.ini inspect tags site.yml --json
lazyansible -C /path/to/ansible -i inventory.ini \
  preview site.yml --tags greeting --json
```

### Preview：觀察目前選擇，不是執行保證

按 `p` 才會觀察／刷新目前 playbook。Preview 保留 **Hosts、Tasks、Tags、Ansible output** 四個區段；`j/k` 選區段，Enter／`l` 進入內容，`j/k` 捲動，`h`／Esc 回區段清單。`[`／`]` 只是切換已有分頁，不會重新發出查詢。

觀察使用同一組 cwd、runtime、inventory、limit、tags 和 extra-vars，呼叫 native `--list-hosts --list-tasks --list-tags`。條件與 dynamic includes 仍可能在真正執行時改變工作，不能把列出的順序／數量當成完整執行圖。這些命令會載入 inventory/plugins，但不執行 playbook tasks。

畫面區分尚未查詢、loading、stale、空匹配和失敗。切換選擇後，先前結果會標成 stale；按 `p` 更新。錯誤或未知輸出格式保留 **Ansible output**，不會把無法解析的資料偽裝成零個 hosts/tasks。查詢失敗時，上一份可用結果也會明確標成 stale。

## 預覽、check mode、真正執行的差別

| 操作 | 會做什麼 |
| --- | --- |
| `Enter` 查看 YAML、開啟 Role Browser | 讀取本機內容；不啟動 playbook 執行 |
| Inventory / Config Inspector | 呼叫 Ansible 的 inventory/config 讀取命令；不是執行 playbook |
| `p` 或 CLI `preview PLAYBOOK` | 呼叫 Ansible 的原生 list 選項，觀察 hosts/tasks/tags；不執行 playbook tasks |
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
4. 按 `p`，先看 Hosts 裡的 localhost、Tasks 裡的 demo role 工作；不應包含 summary task。Enter 進入內容，`h` 回區段。再按 `r`，檢查工作目錄為 `testdata/tutorial`、inventory 為教學檔、limit 為 `localhost`、tags 為 `greeting`。
5. 第一次可直接按 `Enter`：因為預設選的是 Cancel，會取消回來。
6. 再按 `r`，等計畫準備完成，按 `Tab` 選 Run，再按 `Enter` 執行。
7. 確認後會進入 Logs，應看到 `Message supplied by site.yml`。按 `3` 可看 host 狀態。完成訊息包含 exit status；Logs 的上下文保留這次實際執行的選擇。

同樣的流程也能用 CLI 操作：

```sh
# 查 Ansible 的靜態執行範圍
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  preview site.yml --limit localhost --tags greeting

# 只看命令計畫
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  run site.yml --limit localhost --tags greeting --dry-run

# 顯示計畫後，在終端機詢問一次是否執行
./bin/lazyansible -C testdata/tutorial -i inventory.ini \
  run site.yml --limit localhost --tags greeting
```

### 看 role，再體驗「直接跑 role」的差異

按大寫 `O`，會看到 `site.yml` 宣告的 `demo`，而且有 play 名稱、來源行和 `greeting` tag。Enter 看摘要，`f` 查看宣告或 role 原始檔，Esc 分層回來。按 `s` 可把 `greeting` 放進標準 Tags 草稿；取消或套用都會回到 Roles。

此時 `r` 準備的仍是 **site.yml**，因此沿用 play vars，訊息是 `Message supplied by site.yml`。要體驗獨立 role，改用 `:` → 搜尋 `standalone` → **Review standalone role**。Review 會顯示新的 role play，而且 tags 已清空。確認執行後，role 使用自己的 `Default message from role demo`。

產生的 play 不繼承 `site.yml` 的 `gather_facts: false`，所以獨立 role 路徑可能先 Gathering Facts。這項動作不是把父 playbook 裁成一段；先前 tasks、vars、handler 狀態也不會自動補回。

```sh
# 獨立 role 的命令計畫；CLI 只有明確提供 --tags 才會帶入該參數
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
6. 按大寫 `O`，用 `/` 找 `neovim` 宣告；結束篩選後用 Enter 看內容。`f` 查看本機 source，Esc 分層返回。要看其他本機 roles，按 `a` 切成 Project roles；回 Playbooks 按 `2`。這裡不需要獨立執行 role。

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
| 使用本工具尚未提供的 Ansible flags，例如 `--skip-tags`、`--step` | 原生 `ansible-playbook` |
| 觀察選定 playbook 的 hosts/tasks/tags | `p` 或 `lazyansible preview PLAYBOOK` |
| 查看 inventory 解析結果、變更過的 Ansible config | `:` 的 Inspector，或 `lazyansible inspect inventory/config` |
| 看目前用哪個 Ansible、Python、uv tool owner | `:` 搜尋 `runtime`，或 `lazyansible runtime status` |

`:` 的操作方式是先開 Actions，再輸入關鍵字，用 `↑/↓` 選操作、`Enter` 開啟。Runtime 中 `c` 檢查更新，`u` 準備升級計畫；一般開啟畫面不會自動安裝或升級。要在 CLI 僅檢查：

```sh
lazyansible runtime status
lazyansible runtime check
lazyansible runtime upgrade --dry-run
```

更多設定、XDG 路徑與版本管理方式見 [README](../README.md)。完整流程圖、`when` 分支與變數來源追蹤仍列在 [後續評估](../backlog/variable-provenance-graphs.md)，目前的 YAML／role 摘要不表示這些能力已實作。

工作區與靜態 Preview 的設計記錄見 [工作台與執行預覽](../backlog/workspace-execution-ux.md)。單步／局部執行和互動 debugger 仍是另外的研究題目，目前沒有單步除錯按鈕。
