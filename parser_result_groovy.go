package gotreesitter

func normalizeGroovyCompatibility(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "groovy" || len(source) == 0 {
		return
	}
	functionSym, functionNamed, ok := symbolMeta(lang, "function_definition")
	if !ok {
		return
	}
	parameterSym, parameterNamed, ok := symbolMeta(lang, "parameter_list")
	if !ok {
		return
	}
	functionField, hasFunctionField := lang.FieldByName("function")
	parametersField, hasParametersField := lang.FieldByName("parameters")
	bodyField, hasBodyField := lang.FieldByName("body")
	if !hasFunctionField || !hasParametersField || !hasBodyField {
		return
	}

	walkResultTree(root, func(parent *Node) {
		for index := 0; index+1 < resultChildCount(parent); index++ {
			declaration := resultChildAt(parent, index)
			call := resultChildAt(parent, index+1)
			if !groovyUppercaseMethodRecoveryShape(declaration, call, source, lang) {
				continue
			}
			name := declaration.ChildByFieldName("name", lang)
			callee := call.ChildByFieldName("function", lang)
			arguments := call.ChildByFieldName("args", lang)
			body := resultChildAt(arguments, resultChildCount(arguments)-1)

			name.endByte = callee.endByte
			name.endPoint = callee.endPoint

			parameterChildren := resultChildSliceForMutation(arguments)
			parameterChildren = cloneNodeSliceInArena(arguments.ownerArena, parameterChildren[:len(parameterChildren)-1])
			arguments.symbol = parameterSym
			arguments.setNamed(parameterNamed)
			arguments.productionID = 0
			replaceNodeChildrenUnfielded(arguments, parameterChildren)

			functionChildren := append(
				cloneNodeSliceInArena(declaration.ownerArena, resultChildSliceForMutation(declaration)),
				arguments,
				body,
			)
			declaration.symbol = functionSym
			declaration.setNamed(functionNamed)
			declaration.productionID = 0
			replaceNodeChildrenUnfielded(declaration, functionChildren)
			for childIndex, child := range functionChildren {
				switch child {
				case name:
					setNodeChildFieldDirect(declaration, childIndex, functionField)
				case arguments:
					setNodeChildFieldDirect(declaration, childIndex, parametersField)
				case body:
					setNodeChildFieldDirect(declaration, childIndex, bodyField)
				default:
					if child != nil && child.Type(lang) == "identifier" {
						if typeField, ok := lang.FieldByName("type"); ok {
							setNodeChildFieldDirect(declaration, childIndex, typeField)
						}
					}
				}
			}
			replaceChildRangeWithSingleNode(parent, index, index+2, declaration)
		}
	})
}

func groovyUppercaseMethodRecoveryShape(declaration, call *Node, source []byte, lang *Language) bool {
	if declaration == nil || call == nil || declaration.Type(lang) != "declaration" || call.Type(lang) != "function_call" {
		return false
	}
	name := declaration.ChildByFieldName("name", lang)
	callee := call.ChildByFieldName("function", lang)
	arguments := call.ChildByFieldName("args", lang)
	if name == nil || callee == nil || arguments == nil {
		return false
	}
	if name.Type(lang) != "identifier" || callee.Type(lang) != "identifier" || name.endByte != callee.startByte {
		return false
	}
	if name.startByte >= uint32(len(source)) || source[name.startByte] < 'A' || source[name.startByte] > 'Z' {
		return false
	}
	childCount := resultChildCount(arguments)
	if childCount != 3 {
		return false
	}
	open := resultChildAt(arguments, 0)
	close := resultChildAt(arguments, 1)
	body := resultChildAt(arguments, 2)
	return groovyNodeText(open, source, "(") &&
		groovyNodeText(close, source, ")") &&
		body != nil &&
		body.Type(lang) == "closure"
}

func groovyNodeText(node *Node, source []byte, want string) bool {
	if node == nil || node.endByte > uint32(len(source)) || node.startByte > node.endByte {
		return false
	}
	return string(source[node.startByte:node.endByte]) == want
}
