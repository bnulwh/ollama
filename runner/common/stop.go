package common

import (
	"strings"
)

/*
提供了文本生成过程中停止词检测和安全截断的核心工具，关键功能包括：
停止词匹配：完全匹配与部分匹配。
安全截断：保持片段结构，避免无效 Unicode。
UTF-8 校验：确保截断后的文本合法性。
*/

// 检查字符串 sequence 中是否包含任意一个停止词（stops）。
// 用于在文本生成过程中检测是否触发了停止条件。
func FindStop(sequence string, stops []string) (bool, string) {
	for _, stop := range stops {
		if strings.Contains(sequence, stop) {
			return true, stop
		}
	}

	return false, ""
}

// 检查 sequence 的后缀是否部分匹配任意停止词的前缀。
// 避免生成文本的末尾出现不完整的停止词（如停止词是 "stop"，而生成文本以 "st" 结尾）。
func ContainsStopSuffix(sequence string, stops []string) bool {
	for _, stop := range stops {
		for i := 1; i <= len(stop); i++ {
			if strings.HasSuffix(sequence, stop[:i]) {
				return true
			}
		}
	}

	return false
}

// 避免生成文本的末尾出现不完整的停止词（如停止词是 "stop"，而生成文本以 "st" 结尾）。
// 在生成文本时，若检测到停止词，需安全截断以避免生成无效字符。
// truncateStop removes the provided stop string from pieces,
// returning the partial pieces with stop removed, including truncating
// the last piece if required (and signalling if this was the case)
func TruncateStop(pieces []string, stop string) ([]string, bool) {
	// 将片段合并为完整字符串
	joined := strings.Join(pieces, "")

	index := strings.Index(joined, stop)
	if index == -1 {
		// 未找到停止词，直接返回原片段
		return pieces, false
	}
	// 截断到停止词位置
	joined = joined[:index]
	// 按原片段长度重新分割字符串
	// Split truncated string back into pieces of original lengths
	lengths := make([]int, len(pieces))
	for i, piece := range pieces {
		lengths[i] = len(piece)
	}

	var result []string
	tokenTruncated := false
	start := 0
	for _, length := range lengths {
		if start >= len(joined) {
			break
		}

		end := start + length
		if end > len(joined) {
			end = len(joined)
			tokenTruncated = true // 标记最后一个片段被截断
		}
		result = append(result, joined[start:end])
		start = end
	}

	return result, tokenTruncated
}

// 检查字符串末尾是否存在不完整的 UTF-8 字符。
// 避免因截断导致无效的 Unicode 字符（如截断多字节字符的中间字节）
func IncompleteUnicode(token string) bool {
	incomplete := false

	// check if there is incomplete UTF-8 character at the end
	for i := 1; i < 5 && i <= len(token); i++ {
		// 从后向前检查字节
		c := token[len(token)-i]

		if (c & 0xc0) == 0x80 {
			// 跳过连续字节（UTF-8 中间字节）
			// continuation byte: 10xxxxxx
			continue
		}
		// 检查 UTF-8 起始字节的编码长度
		if (c & 0xe0) == 0xc0 {
			// 2-byte character: 110xxxxx ...
			incomplete = i < 2
		} else if (c & 0xf0) == 0xe0 {
			// 3-byte character: 1110xxxx ...
			incomplete = i < 3
		} else if (c & 0xf8) == 0xf0 {
			// 4-byte character: 11110xxx ...
			incomplete = i < 4
		}

		// else 1-byte character or invalid byte
		break
	}

	return incomplete
}
