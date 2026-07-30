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
	parameterItemSym, parameterItemNamed, ok := symbolMeta(lang, "parameter")
	if !ok {
		return
	}
	functionField, hasFunctionField := lang.FieldByName("function")
	parametersField, hasParametersField := lang.FieldByName("parameters")
	bodyField, hasBodyField := lang.FieldByName("body")
	parameterNameField, hasParameterNameField := lang.FieldByName("name")
	if !hasFunctionField || !hasParametersField || !hasBodyField || !hasParameterNameField {
		return
	}

	walkResultTree(root, func(parent *Node) {
		for index := 0; index+1 < resultChildCount(parent); index++ {
			declaration := resultChildAt(parent, index)
			call := resultChildAt(parent, index+1)
			parameterChildren, body, ok := groovyUppercaseMethodRecoveryParts(
				declaration,
				call,
				source,
				lang,
				parameterItemSym,
				parameterItemNamed,
				parameterNameField,
			)
			if !ok {
				continue
			}
			name := declaration.ChildByFieldName("name", lang)
			callee := call.ChildByFieldName("function", lang)
			arguments := call.ChildByFieldName("args", lang)

			name.endByte = callee.endByte
			name.endPoint = callee.endPoint

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

func groovyUppercaseMethodRecoveryParts(
	declaration, call *Node,
	source []byte,
	lang *Language,
	parameterSym Symbol,
	parameterNamed bool,
	parameterNameField FieldID,
) ([]*Node, *Node, bool) {
	if declaration == nil || call == nil || declaration.Type(lang) != "declaration" || call.Type(lang) != "function_call" {
		return nil, nil, false
	}
	name := declaration.ChildByFieldName("name", lang)
	callee := call.ChildByFieldName("function", lang)
	arguments := call.ChildByFieldName("args", lang)
	if name == nil || callee == nil || arguments == nil {
		return nil, nil, false
	}
	if name.Type(lang) != "identifier" || callee.Type(lang) != "identifier" || name.endByte != callee.startByte {
		return nil, nil, false
	}
	if name.startByte >= uint32(len(source)) || source[name.startByte] < 'A' || source[name.startByte] > 'Z' {
		return nil, nil, false
	}
	childCount := resultChildCount(arguments)
	if childCount < 3 {
		return nil, nil, false
	}
	open := resultChildAt(arguments, 0)
	close := resultChildAt(arguments, childCount-2)
	body := resultChildAt(arguments, childCount-1)
	if !groovyNodeText(open, source, "(") ||
		!groovyNodeText(close, source, ")") ||
		body == nil ||
		body.Type(lang) != "closure" {
		return nil, nil, false
	}
	for index := 1; index < childCount-2; index++ {
		child := resultChildAt(arguments, index)
		if child != nil && child.Type(lang) == "identifier" {
			continue
		}
		if !groovyNodeText(child, source, ",") {
			return nil, nil, false
		}
	}
	children := make([]*Node, 0, childCount-1)
	children = append(children, open)
	for index := 1; index < childCount-2; index++ {
		child := resultChildAt(arguments, index)
		if child.Type(lang) == "identifier" {
			parameter := newParentNodeInArena(
				arguments.ownerArena,
				parameterSym,
				parameterNamed,
				cloneNodeSliceInArena(arguments.ownerArena, []*Node{child}),
				[]FieldID{parameterNameField},
				0,
			)
			children = append(children, parameter)
		} else {
			children = append(children, child)
		}
	}
	children = append(children, close)
	return cloneNodeSliceInArena(arguments.ownerArena, children), body, true
}

func groovyNodeText(node *Node, source []byte, want string) bool {
	if node == nil || node.endByte > uint32(len(source)) || node.startByte > node.endByte {
		return false
	}
	return string(source[node.startByte:node.endByte]) == want
}
